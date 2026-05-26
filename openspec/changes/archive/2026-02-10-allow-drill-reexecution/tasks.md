# 容灾演练重跑功能任务清单 (Tasks)

- [ ] /openspec-apply: 实现 allow-drill-reexecution (允许演练重跑)
    - [ ] 修改 `internal/controller/disasterdrill/controller.go` 以支持重跑逻辑
        - [ ] 新增 `HandleRestart` 方法：检查 `testudo.softcdata.com/restart-timestamp` 注解。
        - [ ] **状态重置**：如果检测到重跑请求且演练处于终态（`Completed/Failed`）：
            - [ ] 重置 `Status.State` 为 `Pending`。
            - [ ] 清空其他状态字段 (`Message`, `StartTime`, `OperationName`, `Steps` 等)。
            - [ ] 强制重置 `Spec.Confirmed = false`，等待用户再次确认。
        - [ ] **Operation 名称生成**：修改 `DisasterOperation` 命名逻辑，支持时间戳或随机后缀（例如 `drill-{name}-{timestamp}`），避免与旧 Operation 冲突。
    - [ ] 在 `disaster-server` 中实现 `RestartDrill` 接口
        - [ ] 新增 POST `/drills/{name}/restart` 接口。
        - [ ] 逻辑：校验演练是否处于终态。如果是，设置 `testudo.softcdata.com/restart-timestamp` 注解。
    - [ ] 编写测试用例验证重跑流程。
