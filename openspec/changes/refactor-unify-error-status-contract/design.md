# Design: 统一错误状态语义（Error Reason/Message Contract）

## 1. 背景问题

当前错误表达存在三类核心割裂：

1. 字段割裂：`status.reason/message`、`status.message`、`conditions[].reason/message` 并存。
2. 语义割裂：`reason` 既有稳定枚举，也有完整句子。
3. 观测割裂：状态字段与 Warning 事件语义未稳定对齐。

结果是 server/web 需要写大量资源特判，自动化排障与告警规则难以稳定复用。

## 2. 目标契约

### 2.1 字段语义

- `reason`：机器可读错误码，稳定、可枚举、可跨服务映射。
- `message`：人类可读错误描述，包含上下文细节。

### 2.2 编码规则

- `reason` MUST 使用 PascalCase。
- `reason` MUST 仅包含 `[A-Za-z0-9]`。
- `reason` MUST NOT 包含空格、标点、自然语言句子。
- `message` SHOULD 包含最小可定位上下文（资源名、步骤名、超时值、外部错误摘要）。

### 2.3 清理规则

- 资源进入成功/就绪终态时，必须清理与当前状态不一致的陈旧错误信息。
- 长任务中可保留最近错误，但不得覆盖已确认成功状态。

## 3. 模块改造矩阵

| 模块 | 当前 | 目标 |
| --- | --- | --- |
| Cluster | `reason + message`，但命名未完全收敛 | 保持字段，统一码表与清理策略 |
| StorageRepository | `reason + message` | 保持字段，统一码表 |
| AppBackup / AppRestore | `reason + message`，成功路径常清空 | 保持字段，统一设置/清理入口 |
| DisasterConfig | 仅 `reason` | 补 `message`，`reason` 改稳定码 |
| DataSync / ResourceSync | 以 condition reason/message 为主 | 保持 condition，补统一顶层错误出口或固定映射 |
| DisasterOperation | 仅 `message` + step message | 补 `reason`，步骤失败映射统一码 |
| DisasterDrill | 仅 `message` | 补 `reason` |
| DisasterInstance | 以状态机与子资源聚合为主 | 已补 `status.reason/status.message`，用于透出初始化失败语义 |
| DisasterGroup | 以聚合状态为主 | 已补 `status.reason/status.message`，聚合组级失败语义 |
| DisasterPolicy | `reason/message` 字段存在但错误路径覆盖不足 | 补齐 `InvalidSchedule` 等稳定错误语义并清理 stale error |
| DisasterBackup / DisasterJob | legacy，已废弃 | 不纳入本提案改造，仅保留废弃标注 |

### 3.1 2026-03-23 明确适配缺口（执行基线）

| 位置 | 当前缺口 | 目标 |
| --- | --- | --- |
| operator `DisasterConfig` controller | `reason` 含自然语言/动态字符串 | `reason` 改为稳定错误码，细节下沉到 `message`（已完成） |
| operator `DisasterGroup` | 缺少统一组级错误出口 | 补 `status.reason/status.message`，按成员聚合失败语义（已完成） |
| server `DisasterConfigStatusDTO` | 无 `message` | 补 `message` 并映射 `status.message`（已完成） |
| server `SubResourceStatusDTO` (`sync-status`) | 无 `reason/message` | 补 `reason/message` 并映射 `DataSync/ResourceSync.status`（已完成） |
| server `DisasterDrillDTO` | 顶层扁平 `state`，无统一 `status` 对象 | 改为 `status.state/status.reason/status.message`（已完成） |
| server `DisasterOperationDTO` (group watch) | 无 `reason` | 补 `reason` 并映射 `operation.status.reason`（已完成） |
| server `DisasterGroupStatusDTO` | 无 `reason/message` | 补 `reason/message` 并映射组级错误出口（已完成） |
| operator `DisasterPolicy` controller | 无效 schedule 仅发 Event，无稳定错误状态 | 收敛为 `reason=InvalidSchedule` + `message`，并在恢复后清理（已完成） |
| operator `AppBackup/AppRestore/StorageRepository` 失败任务事件 | `errorCode/message` 与状态语义存在偏差 | 对齐失败事件 `errorCode` 与状态 reason，并统一 message 语义（已完成） |
| operator `DisasterJob/DisasterBackup` | 历史 legacy 模块字段不完整 | 明确标注为废弃模块，不纳入本提案实施（已确认） |

## 4. 控制器实现模式

统一通过 helper 设置与清理：

- `SetStatusError(obj, code, msg)`：写入 status 错误字段。
- `SetConditionError(obj, condType, code, msg)`：写入条件错误并保持映射一致。
- `ClearStatusError(obj)`：成功终态清理陈旧错误。

这样可避免每个控制器重复手写逻辑导致的不一致。

## 5. 事件对齐策略

不改变全局事件 `Reason=ExecutionStarted/Progress/Finished` 的既有规范。

在 `ExecutionFinished` 且失败时，JSON payload 增加：

- `errorCode`：来源于 `status.reason` 或最新失败 condition reason。
- `message`：与 `status.message` 或最新失败 condition message 保持一致。

## 6. 迁移策略

### 6.1 分批迁移

- 第 1 批：核心 V2 编排链路（Cluster/AppBackup/AppRestore/DataSync/ResourceSync/Operation/Drill）。
- 第 2 批：其余 active 控制器。
- 第 3 批：legacy 链路。

### 6.2 兼容策略

- 新字段均为可选，避免 CRD 破坏性升级。
- 过渡期允许 server 同时读取旧语义和新语义。
- 完成后再统一清理 legacy 逻辑。

## 7. 测试策略

- 单测：错误路径断言 `reason/message` 与 condition 语义。
- 状态清理测试：失败 -> 成功后 stale error 清空。
- 事件测试：失败终态 `ExecutionFinished` 载荷带 `errorCode` 且对齐状态。
- 回归测试：关键资源状态机主路径不受影响。
