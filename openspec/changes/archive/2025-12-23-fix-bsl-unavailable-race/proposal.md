# Proposal: 修复 AppBackup 因 BSL 初始化延迟导致的失败

## Summary
修复 `AppBackup` 在创建时，由于 Velero `BackupStorageLocation` (BSL) 尚未完成验证（处于 `Unavailable` 或初始化状态）而直接失败的问题。

## Motivation
用户报告在创建新的集群和存储后，立即创建 `AppBackup` 会导致失败，错误信息为 "backup can't be created because BackupStorageLocation ... is in Unavailable status"。
然而，稍后检查 Velero BSL 状态发现其已变为 `Available`。这表明存在竞态条件：Operator 在 BSL 完成首次验证前就尝试使用它，导致过早失败。

## Root Cause Analysis
`AppBackup` 控制器在协调过程中，会检查指定的 BSL 状态。如果 BSL 尚未准备好（Velero 需要时间连接对象存储并验证），控制器可能直接将 `AppBackup` 标记为 `Failed`，而不是等待 BSL 就绪。

## Proposed Changes

### 1. 增加 BSL 就绪等待机制
- 在 `AppBackup` 控制器（`PendingHandler` 或相关逻辑）中，当检测到 BSL 处于 `Unavailable` 状态时：
    - **不要** 立即将 `AppBackup` 状态设置为 `Failed`。
    - **应该** 记录一个 Warning Event (e.g., `WaitingForBSL`)。
    - **应该** 返回 `RequeueAfter`（例如 5-10 秒），以便稍后重试。
- 设置一个超时时间（例如 1-2 分钟），如果超时后 BSL 仍不可用，再标记为 `Failed`。

### 2. 优化错误处理
- 区分 "BSL 不存在" 和 "BSL 存在但不可用"。
- 对于 "BSL 不存在"，保持现有逻辑（可能尝试创建或报错）。
- 对于 "BSL 不可用"，进入等待循环。

### 3. 修改 `DefaultBSL.ApplyStorageRepository`
- 修改 `updateBackupStorageLocation` 方法：
    - 检查 BSL 的 `Status.Phase`。
    - 如果 Phase 为 `Unavailable`，返回一个特定的错误（例如 `ErrBSLUnavailable`）或直接返回错误信息，提示调用者重试。
    - 在 `AppBackup` 的 `PendingHandler` 中捕获此错误，并返回 `RequeueAfter`。

## Tasks
- [ ] 修改 `internal/controller/BSL.go` 中的 `updateBackupStorageLocation`，当 BSL 不可用时返回错误。
- [ ] 修改 `internal/controller/appbackup/appbackup_pending.go`，处理 BSL 不可用的错误，执行 Requeue。

