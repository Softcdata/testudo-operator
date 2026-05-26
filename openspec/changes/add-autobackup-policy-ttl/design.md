# Design: Velero 自动备份策略 TTL

## Context

现有模型中：

- `AppBackup.spec.template` 直接复用 `velerov1.BackupSpec`，其中已经包含 `ttl`。
- `AppBackup` 创建 Velero `Schedule` 时，会把 `appBackup.spec.template` 写入 `Schedule.spec.template`。
- `DisasterPolicy` 当前只包含 `type/schedule/startTime/description/state`，自动备份策略只能表达 cron，无法表达 retention。
- Server 的 `DisasterPolicyDTO` 与前端策略表单未包含 `ttl`；前端 AppBackup 详情页已有 `detail.spec.ttl` 展示位。

因此本设计不新增新的 retention 抽象，而是把策略级 TTL 映射到 Velero 原生 `BackupSpec.TTL`。

## Goals

- 在 `AutoBackup` 策略上表达 Velero Backup 保留时间。
- 保证使用策略创建/更新的 `AppBackup` 与 Velero `Schedule` 获得同一个 TTL。
- 保证策略详情和自动备份详情能回显 TTL。
- 保持存量策略和手动 AppBackup 兼容。

## Non-Goals

- 不设计按备份历史记录独立修改 TTL。
- 不设计基于存储容量的动态 TTL。
- 不改变 Velero 删除过期 Backup 的执行机制。
- 不把 TTL 应用于 DataSync/ResourceSync 策略。

## Data Model

### `DisasterPolicySpec`

新增字段：

```go
// TTL defines how long Velero backups created by this AutoBackup policy are retained.
// Only effective when type is AutoBackup.
// +optional
TTL *metav1.Duration `json:"ttl,omitempty"`
```

采用指针的理由：

- 区分未配置与显式零值。
- 便于 Server 做部分更新时保留/清空字段。
- 与 `AppBackupSpec.Timeout` 等可选 duration 字段风格一致。

### Server DTO

策略 DTO 与请求结构新增：

```go
type DisasterPolicySpecDTO struct {
    Type     string           `json:"type"`
    Schedule string           `json:"schedule"`
    TTL      *metav1.Duration `json:"ttl,omitempty"`
    // ...
}

type CreateDisasterPolicyRequest struct {
    // ...
    TTL *metav1.Duration `json:"ttl,omitempty"`
}

type UpdateDisasterPolicyRequest struct {
    // ...
    TTL *metav1.Duration `json:"ttl,omitempty"`
    ClearTTL *bool `json:"clearTTL,omitempty"` // 可选实现；若不引入 clearTTL，则需用字段存在性跟踪支持清空
}
```

更新请求需要支持“未传 TTL 保留旧值”和“显式清空 TTL”。实现可二选一：

- 使用 `clearTTL=true` 表示清空。
- 或采用自定义 `UnmarshalJSON` 跟踪字段存在性。

若前端首期不提供清空 TTL，仍必须保证未传 TTL 不会误清空已有 TTL。

## Behavior

### Policy Validation

`DisasterPolicy` 创建/更新必须满足：

- `schedule` 仍按现有 cron 校验。
- 当 `type=AutoBackup` 且 `ttl` 非空时，`ttl.Duration > 0`。
- 当 `type=DataSync` 或 `type=ResourceSync` 时：
  - Server 提交期应拒绝携带 `ttl`。
  - Operator 防御性忽略 `ttl`，不得影响 DS/RS schedule。

错误建议：

- `reason=InvalidTTL`
- message 包含字段名 `ttl` 与非法值原因。

### AppBackup Inheritance

在 `appbackup_ready.go` 的策略分支中：

1. 读取 `AppBackup.spec.disasterPolicy`。
2. 若策略为 `AutoBackup`：
   - `appBackup.Spec.Schedule = policy.Spec.Schedule`
   - 若 `policy.Spec.TTL != nil`，则 `appBackup.Spec.Template.TTL = policy.Spec.TTL`
   - 写入策略 UID 标签，保持现有删除保护与关联关系。
3. 若策略非 `AutoBackup`，保持现有失败/忽略语义，不从该策略派生 TTL。

Velero Schedule 创建与更新路径不需要新字段映射，只要继续比较并更新 `schedule.Spec.Template` 即可；`BackupSpec.TTL` 会随 `Template` 一起进入 `Schedule.spec.template.ttl`。

### Detail Echo

策略详情：

- `ConvertSpecToDTO` 必须把 `spec.TTL` 写入 `dto.Spec.TTL`。
- 创建/更新响应必须包含 TTL。
- 列表响应必须包含 TTL。

自动备份详情：

- 对 `AppBackup` DTO 暴露 `spec.ttl`，来源为 `appBackup.Spec.Template.TTL`。
- 使用策略的 AppBackup 必须在 controller reconcile 后把策略 TTL 写入 `AppBackup.spec.template.ttl`，从而详情接口无需再实时 join 策略也能稳定回显。
- 若详情接口为了降低等待窗口选择实时补充策略 TTL，也必须以 `AppBackup.spec.template.ttl` 为执行事实，以策略 TTL 作为未收敛时的辅助回显，并在响应中避免覆盖 CR 本身。

历史备份：

- `status.history[].expiration` 表示 Velero 根据 TTL 计算出的实际过期时间。
- 不新增 `history[].ttl`，避免在历史记录中存储可由过期时间与开始时间推导但并不稳定的展示字段。

## API Examples

创建自动备份策略：

```json
{
  "name": "auto-daily-30d",
  "type": "AutoBackup",
  "schedule": "0 2 * * *",
  "ttl": "720h",
  "state": "Enabled",
  "description": "每日自动备份，保留 30 天"
}
```

策略详情响应：

```json
{
  "name": "auto-daily-30d",
  "spec": {
    "type": "AutoBackup",
    "schedule": "0 2 * * *",
    "ttl": "720h",
    "state": "Enabled",
    "description": "每日自动备份，保留 30 天"
  }
}
```

自动备份详情响应：

```json
{
  "name": "app-backup-prod",
  "spec": {
    "schedule": "0 2 * * *",
    "disasterPolicy": "auto-daily-30d",
    "ttl": "720h"
  },
  "status": {
    "history": [
      {
        "name": "app-backup-prod-202604280200",
        "expiration": "2026-05-28T02:00:00Z"
      }
    ]
  }
}
```

## Rollout

1. Operator 增加 CRD 字段、校验与策略继承。
2. Server 更新 DTO/request/response 与策略 TTL 校验。
3. Web 策略表单增加 TTL 输入，仅在 `AutoBackup` 类型显示；策略详情/列表回显 TTL。
4. Chart 发布新 CRD。
5. 用 e2e 验证策略创建、AppBackup 策略继承、Velero Schedule 模板 TTL 与详情回显一致。

## Risks

- 风险：策略更新后已存在 AppBackup 的 TTL 是否立即变化。
  - 决策：遵循现有策略更新保护；如果被 AppBackup 引用时禁止更新，则不会发生运行中策略 TTL 变更。后续若放开策略更新，需要单独设计 fan-out 或周期性收敛。
- 风险：前端自动备份向导已有 TTL 输入，且选择策略时也会提交 TTL。
  - 决策：策略 TTL 优先；前端在选择 AutoBackup 策略时应展示策略 TTL，并避免让用户误以为可用 AppBackup 表单覆盖策略 TTL。
- 风险：非 AutoBackup 策略携带 TTL。
  - 决策：Server fail-fast 拒绝，Operator 防御性忽略。
