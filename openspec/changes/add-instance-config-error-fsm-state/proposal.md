# Change: 为容灾实例增加 ConfigError 状态并保证恢复回原状态

## Why

当前 `DisasterInstance.status.fsmState` 与 `DisasterConfig.status.status` 独立演进。配置异常后，实例常驻 `Protected`，用户会误判实例与容灾组健康度。

本变更的硬约束是：
1. 当 `DisasterConfig` 异常时，实例必须进入 `ConfigError`。
2. 当配置恢复为 `Ready` 时，实例必须恢复为进入 `ConfigError` 前的原始状态。
3. 容灾组聚合必须把 `ConfigError` 成员计入错误集合。

## What Changes

### 1) 数据模型

- 在 `pkg/apis/disaster/v1/disasterinstance_types.go` 新增：
  - `FsmStateConfigError = "ConfigError"`
  - `DisasterInstanceStatus.LastStableFsmState string`
- `LastStableFsmState` 用途限定为“记录进入 `ConfigError` 前状态”，不用于其他流程。

### 2) 实例控制器状态机改造

- 在 `internal/controller/disasterinstance/controller.go` 新增统一配置健康判定函数。
- 在 `Reconcile` 状态路由前加入“配置守卫”。
- 配置异常命中条件：
  - 引用的 `DisasterConfig` 不存在。
  - `DisasterConfig.status.status == Error`。
  - `DisasterConfig.status.status == NotReady`。
- 首次进入 `ConfigError` 时写入 `LastStableFsmState`。
- 配置恢复时从 `ConfigError` 严格恢复到 `LastStableFsmState`。
- 若 `LastStableFsmState` 为空，不允许恢复到猜测状态，保持 `ConfigError` 并给出确定错误原因。

### 3) 容灾组聚合改造

- 在 `internal/controller/disastergroup/controller.go` 将 `ConfigError` 纳入错误成员统计。
- `ReadyInstances` 继续只统计 `Protected`，禁止把 `ConfigError` 计为就绪。

### 4) 回归与单元测试

- 新增实例状态进入、保持、恢复、空记忆阻断恢复、操作态不拦截测试。
- 新增容灾组聚合识别 `ConfigError` 测试。

## Non-Goals

- 不处理前端按钮禁用策略（由前端独立控制）。
- 不新增容灾操作类型。
- 不修改 `DisasterOperation` 现有编排逻辑。

## Impact

- Affected specs:
  - `disaster-instance`
  - `disaster-group`
- Affected code:
  - `pkg/apis/disaster/v1/disasterinstance_types.go`
  - `internal/controller/disasterinstance/controller.go`
  - `internal/controller/disasterinstance/controller_test.go`
  - `internal/controller/disastergroup/controller.go`
  - `internal/controller/disastergroup/controller_test.go`
