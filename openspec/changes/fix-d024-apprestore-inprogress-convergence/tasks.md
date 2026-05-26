## 1. 规范与设计

- [x] 1.1 明确 D-024 收敛判定条件（`InProgress + itemsRestored==totalItems + 无 completionTimestamp + 超过 grace`）
- [x] 1.2 定义自动自愈与失败收敛边界（最多一次自动重试）
- [x] 1.3 定义新增事件码与 reason/message 约定

## 2. 控制器实现

- [x] 2.1 在 `Restoring` 分支新增 `ProgressCompleteGrace` 软收敛逻辑
- [x] 2.2 增加“单次自动重试”状态记录与幂等保护
- [x] 2.3 去除 `restorePVs` 对 PVR 检查的门控，仅按运行态执行最佳努力探测
- [x] 2.4 在重试后再次卡住时进入 `PhaseFailed` 并设置标准 reason/message
- [x] 2.5 增加 D-025 启动卡死判定（phase 空/`New` 且无 start/completion 超时）
- [x] 2.6 增加 D-025 `during the server starting` 瞬态失败自动重试逻辑
- [x] 2.7 调整 `NotFound` 分支，支持自动重试后的预期重建

## 3. 可观测性

- [x] 3.1 增加事件：`RestoreProgressStalled`
- [x] 3.2 增加事件：`RestoreAutoRetryTriggered`
- [x] 3.3 增加事件：`RestoreStalledAfterRetry`
- [x] 3.4 增加事件：`RestoreStartupStalled`

## 4. 测试与验收

- [x] 4.1 单测：`InProgress` 且 `14/14` 长时间无 completion 触发收敛逻辑
- [x] 4.2 单测：首次卡住触发自动重试，第二次卡住转 `Failed`
- [x] 4.3 单测：`restorePVs=false` 场景仍执行 PVR 异常探测（存在 PVR 时）
- [x] 4.4 单测：`NotFound + auto-retry` 路径可重建，不误判异常删除
- [ ] 4.5 包级回归：`go test ./internal/controller/apprestore -count=1`（当前存在与本次修改无关的历史失败）
- [x] 4.6 `openspec validate fix-d024-apprestore-inprogress-convergence --strict`
