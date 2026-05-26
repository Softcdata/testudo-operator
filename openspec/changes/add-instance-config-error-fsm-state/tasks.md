## 1. CRD 与状态字段

- [x] 1.1 修改 `pkg/apis/disaster/v1/disasterinstance_types.go`，新增 `FsmStateConfigError` 常量。
- [x] 1.2 在 `DisasterInstanceStatus` 新增 `LastStableFsmState` 字段与中文注释。
- [x] 1.3 运行 `make generate`，确认 `zz_generated.deepcopy.go` 同步包含新字段。
- [x] 1.4 运行 `make manifests`，确认 CRD schema 包含 `status.lastStableFsmState`。

## 2. 实例控制器配置守卫

- [x] 2.1 在 `internal/controller/disasterinstance/controller.go` 新增配置健康判定函数 `evaluateConfigHealth`。
- [x] 2.2 在 `Reconcile` 中 `FsmState` 初始化后、`switch` 路由前调用 `guardByConfigHealth`。
- [x] 2.3 在 `guardByConfigHealth` 中实现进入 `ConfigError` 逻辑，覆盖 `Pending/Initializing/Protected/Paused/Active`。
- [x] 2.4 在首次进入 `ConfigError` 时写入 `LastStableFsmState`，禁止在保持阶段覆盖。
- [x] 2.5 在保持 `ConfigError` 时刷新 `reason/message` 并清空 `availableOperations`。
- [x] 2.6 在配置恢复分支按 `LastStableFsmState` 精确恢复，恢复后清空 `reason/message/lastStableFsmState`。
- [x] 2.7 在 `LastStableFsmState` 为空时阻断恢复，保持 `ConfigError` 并写入 `reason=LastStableStateMissing`。
- [x] 2.8 确保 `FailingOver/FailingBack/Failed` 不受该守卫接管。

## 3. 容灾组聚合

- [x] 3.1 修改 `internal/controller/disastergroup/controller.go` 错误成员判定：新增 `FsmStateConfigError`。
- [x] 3.2 保持 `readyInstances` 仅统计 `FsmStateProtected`。
- [x] 3.3 确认 `summarizeInstanceError` 在 `ConfigError` 场景输出包含状态标签。

## 4. 单元测试补齐

- [x] 4.1 在 `internal/controller/disasterinstance/controller_test.go` 新增“配置不存在 -> ConfigError”测试。
- [x] 4.2 新增“配置 NotReady -> ConfigError（透传 reason/message）”测试。
- [x] 4.3 新增“ConfigError 保持时不覆盖 LastStableFsmState”测试。
- [x] 4.4 新增“配置恢复 -> 恢复 Protected”测试。
- [x] 4.5 新增“配置恢复 -> 恢复 Paused”测试。
- [x] 4.6 新增“配置恢复 -> 恢复 Active”测试。
- [x] 4.7 新增“LastStableFsmState 为空时阻断恢复”测试。
- [x] 4.8 新增“FailingOver 不被 ConfigGuard 覆盖”测试。
- [x] 4.9 在 `internal/controller/disastergroup/controller_test.go` 新增“ConfigError 成员触发组错误”测试。
- [x] 4.10 在 `internal/controller/disastergroup/controller_test.go` 新增“ConfigError 不计入 ready”测试。

## 5. 验证与交付

- [x] 5.1 运行 `go test ./internal/controller/disasterinstance -count=1`。
- [x] 5.2 运行 `go test ./internal/controller/disastergroup -count=1`。
- [x] 5.3 运行 `go test ./internal/controller/... -count=1`。
- [ ] 5.4 运行 `make test`。
- [x] 5.5 运行 `openspec validate add-instance-config-error-fsm-state --strict`。
