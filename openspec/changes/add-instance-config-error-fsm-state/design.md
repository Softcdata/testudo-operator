## Context

`DisasterInstanceReconciler` 当前在 `Reconcile` 内直接按 `fsmState` 路由。配置异常在 `Pending` 以外状态不会被显式透出，`DisasterGroup` 聚合也无法识别该类异常。

用户要求的核心行为是确定性的双向状态机：
1. 配置异常时立刻进入 `ConfigError`。
2. 配置恢复后必须回到进入前状态，不允许恢复到猜测状态。

## 设计目标

- 目标 1：配置异常信息单点判定，避免各 `handle*` 分支重复逻辑。
- 目标 2：状态恢复路径可回放，可验证。
- 目标 3：组聚合对 `ConfigError` 输出稳定一致。

## 数据模型

### 新增状态常量

文件：`pkg/apis/disaster/v1/disasterinstance_types.go`

- 新增 `FsmStateConfigError = "ConfigError"`。

### 新增状态记忆字段

文件：`pkg/apis/disaster/v1/disasterinstance_types.go`

- 在 `DisasterInstanceStatus` 新增：
  - `LastStableFsmState string 'json:"lastStableFsmState,omitempty"'`
- 语义约束：
  - 仅在“从稳定状态首次切入 `ConfigError`”时写入。
  - 在 `ConfigError` 保持阶段禁止覆盖。
  - 成功恢复后清空。

## 控制器改造（函数级）

文件：`internal/controller/disasterinstance/controller.go`

### 新增函数

1. `evaluateConfigHealth(ctx, instance) (healthy bool, reason string, message string, err error)`
- 输入：实例对象。
- 输出：配置健康性与错误语义。
- 判定顺序：
  1. 读取 `DisasterConfig`。
  2. `NotFound` -> `healthy=false`, `reason=ConfigNotFound`, `message=DisasterConfig <name> not found`。
  3. `status=Error` -> `healthy=false`，优先透传 `config.status.reason/message`，缺省分别兜底 `ConfigError` 与 `DisasterConfig <name> status=Error`。
  4. `status=NotReady` -> `healthy=false`，优先透传 `config.status.reason/message`，缺省分别兜底 `ConfigNotReady` 与 `DisasterConfig <name> status=NotReady`。
  5. `status=Ready` -> `healthy=true`。

2. `guardByConfigHealth(ctx, log, instance) (handled bool, result ctrl.Result, err error)`
- 作用：在状态机路由前执行，负责进入、保持、恢复 `ConfigError`。

### Reconcile 接入点

在以下代码块之间插入守卫：
1. `FsmState` 初始化完成后。
2. `switch instance.Status.FsmState` 之前。

伪代码：

```go
if instance.Status.FsmState == "" {
    // 现有初始化逻辑
}

handled, result, err := r.guardByConfigHealth(ctx, log, instance)
if err != nil {
    return ctrl.Result{}, err
}
if handled {
    return result, nil
}

switch instance.Status.FsmState {
    // 现有分支
}
```

## 状态机细则

### 可被配置守卫接管的状态

- `Pending`
- `Initializing`
- `Protected`
- `Paused`
- `Active`
- `ConfigError`

### 明确不接管的状态

- `FailingOver`
- `FailingBack`
- `Failed`

说明：进行中操作态由 `DisasterOperation` 控制器主导，不在本次守卫接管范围内。

### 转移矩阵

| 当前状态 | 配置判定 | 动作 | 目标状态 |
|---|---|---|---|
| Pending/Initializing/Protected/Paused/Active | 不健康 | 首次进入时记录 `LastStableFsmState`，写入错误语义 | ConfigError |
| ConfigError | 不健康 | 刷新 `reason/message`，保持 `LastStableFsmState` | ConfigError |
| ConfigError | 健康且 `LastStableFsmState` 非空 | 恢复、清理错误、清空记忆字段 | LastStableFsmState |
| ConfigError | 健康且 `LastStableFsmState` 为空 | 保持错误态并写入 `LastStableStateMissing` | ConfigError |

### 恢复硬约束

- 恢复目标必须严格等于 `LastStableFsmState`。
- 禁止在 `LastStableFsmState` 为空时恢复到 `Protected`、`Pending`、`Active` 这类猜测状态。

## 状态字段写入规则

进入 `ConfigError`：
- `status.fsmState = ConfigError`
- `status.lastStableFsmState = <进入前状态>`
- `status.reason/status.message = <配置异常语义>`
- `status.availableOperations = []`

保持 `ConfigError`：
- `status.fsmState = ConfigError`
- `status.lastStableFsmState` 不改
- `status.reason/status.message` 按最新配置状态刷新
- `status.availableOperations = []`

恢复成功：
- `status.fsmState = status.lastStableFsmState`
- 清空 `status.reason/status.message`
- 清空 `status.lastStableFsmState`
- 不在该分支计算可用操作，交由下一轮既有 `handle*` 分支计算，避免重复逻辑。

## 容灾组聚合更新

文件：`internal/controller/disastergroup/controller.go`

错误成员判定从：
- `fsmState == Failed || reason != ""`

更新为：
- `fsmState == Failed || fsmState == ConfigError || reason != ""`

`readyInstances` 统计条件保持：
- 仅 `fsmState == Protected` 计入 ready。

## 与现有控制器的兼容性

- `DisasterOperationReconciler` 无代码改动。
- 当实例从 `ConfigError` 恢复后，操作可用集由原有状态处理器产生。
- 当实例保持 `ConfigError`，可用操作为空，避免误触发操作。

## 验证策略

### 单元测试（必须新增）

文件：`internal/controller/disasterinstance/controller_test.go`

- `ConfigNotFound`：`Protected -> ConfigError`，写入 `LastStableFsmState=Protected`。
- `ConfigNotReady`：`Paused -> ConfigError`，透传 reason/message。
- `ConfigError 保持`：连续调谐不覆盖 `LastStableFsmState`。
- `恢复到原状态`：
  - `LastStable=Protected` 恢复到 `Protected`
  - `LastStable=Paused` 恢复到 `Paused`
  - `LastStable=Active` 恢复到 `Active`
- `缺失记忆阻断恢复`：`LastStableFsmState=""` 时保持 `ConfigError` 并写入 `LastStableStateMissing`。
- `进行中操作态不接管`：`FailingOver` 遇到配置异常不改写为 `ConfigError`。

文件：`internal/controller/disastergroup/controller_test.go`

- `ConfigError` 成员触发组错误聚合。
- `ConfigError` 成员不计入 `ReadyInstances`。

### 命令验证

- `go test ./internal/controller/disasterinstance -count=1`
- `go test ./internal/controller/disastergroup -count=1`
- `go test ./internal/controller/... -count=1`
- `openspec validate add-instance-config-error-fsm-state --strict`
