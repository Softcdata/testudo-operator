# 允许容灾演练重新执行 (Allow Drill Re-execution)

## 背景与问题 (Context)

目前，`DisasterDrill` 资源被设计为**一次性执行**。一旦进入终态（`Completed` 或 `Failed`），控制器停止调谐，且 API 拒绝再次确认执行。这导致用户为了重复相同的演练配置（例如故障排查或定期验证），必须每次都创建新的 `DisasterDrill` 资源，不仅造成资源冗余，也不利于长期跟踪演练配置。

用户希望：**若演练在非运行中（已完成或失败），可以再次执行该演练。**

## 核心问题 (Key Issues)

1.  **不可变执行设计**：当前 `DisasterDrill` 的 Status 仅记录单词执行结果，重跑会覆盖历史状态。
2.  **Operation 冲突**：底层的 `DisasterOperation` 目前与 Drill 是一对一关联（通过 Label 和固定命名规则）。简单的重跑会直接复用旧的 Operation，导致状态混淆。

## 解决方案 (Proposed Solution)

引入 **重启 (Restart)** 机制，允许重置处于终态的演练。

### 1. API 变更

-   新增 `POST /drills/{name}/restart` 接口。
-   该接口将在 `DisasterDrill` 资源上添加注解 `testudo.softcdata.com/restart-timestamp`，作为重跑信号。

### 2. 控制器逻辑变更

-   **监听重跑信号**：控制器检查 `restart-timestamp` 注解。
-   **状态重置**：
    -   若当前状态为终态且收到新的重跑信号：
        -   清除 `Status.State`, `Status.Message`, `Status.OperationName` 等字段，将其重置为 `Pending`。
        -   **强制重置** `Spec.Confirmed = false`，要求用户重新确认执行（防止意外循环）。
-   **Operation 命名优化**：
    -   修改 `DisasterOperation` 命名规则，从 `drill-{name}` 改为包含随机后缀或时间戳的唯一名称（例如 `drill-{name}-{timestamp}`）。
    -   这样每次重跑都会创建新的 Operation，旧的 Operation 最为历史记录保留（可被自动清理或手动查询）。

### 3. CRD 变更

-   Status 字段结构保持不变，但允许被清空重置。
-   未来可扩展 `Status.History` 字段以在 Drill 及其内部展示历史执行列表。

## 实施计划 (Implementation)

1.  **Controller**: 修改 `internal/controller/disasterdrill/controller.go`，实现重跑检测与状态重置逻辑。
2.  **Controller**: 修改 `generateOperationName` 方法，支持生成唯一 Operation 名称。
3.  **Server**: 实现 `restartDrill` API 接口。
4.  **Verification**: 编写测试用例验证重跑流程。
