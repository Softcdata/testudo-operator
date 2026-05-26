# Proposal: Optimize Velero Schedule Reconciliation

## Why
目前的 `AppBackup` 调和逻辑中，在创建 `Velero Schedule` 后会继续执行后续逻辑，可能导致状态更新不及时或逻辑冗余。
此外，对 `Velero Schedule` 的更新操作（如同步 `Paused` 状态）未进行全面的差异比对，可能导致不必要的 Kubernetes API 调用（Update），增加 API Server 负担。

## What Changes
- **创建后立即返回**: 当 `Velero Schedule` 是新创建的时候，更新 `AppBackup` 状态并立即返回，触发下一次 Reconcile。
- **按需更新**: 在更新 `Velero Schedule` 之前，比较 `AppBackup.Spec` 和现有的 `Velero Schedule.Spec`，仅在检测到变更时执行 Update 操作。
