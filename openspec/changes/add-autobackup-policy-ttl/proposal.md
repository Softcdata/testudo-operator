# Change: 为 Velero 自动备份策略增加 TTL 并在详情中回显

## Why

当前系统已在直接创建 `AppBackup` 时支持 `spec.template.ttl`，前端资源备份向导也会提交 TTL；但当用户选择“自动备份策略”时，策略 `DisasterPolicy` 只保存 `type/schedule/startTime/description/state`，缺少策略级 TTL。

这带来两个问题：

- 用户无法在自动备份策略中定义 Velero Backup 的保留时间。
- 使用策略创建的自动备份详情中无法稳定回显策略 TTL，只能看到调度表达式，无法判断由该策略生成的备份会保留多久。

本变更将 TTL 纳入 `AutoBackup` 策略契约，并要求该 TTL 透传到 Velero `Schedule.spec.template.ttl`，同时在策略详情和自动备份详情中回显。

## What Changes

### 1. `DisasterPolicy.spec.ttl`

在 `DisasterPolicySpec` 新增可选字段：

- `ttl`: `metav1.Duration`，JSON 字段名为 `ttl`
- 语义：Velero 自动备份保留时间，最终写入 `velerov1.BackupSpec.TTL`
- 仅当 `spec.type == AutoBackup` 时生效

建议示例：

```yaml
apiVersion: testudo.softcdata.com/v1
kind: DisasterPolicy
metadata:
  name: auto-daily-30d
spec:
  type: AutoBackup
  schedule: "0 2 * * *"
  ttl: 720h
  state: Enabled
```

### 2. 策略校验

- `AutoBackup` 策略允许配置 `ttl`。
- `ttl` 非空时必须能被 Kubernetes `metav1.Duration` / Go `time.ParseDuration` 解析。
- `ttl <= 0` 必须拒绝。
- `DataSync` / `ResourceSync` 策略不得使用 `ttl` 影响调度；首期建议在 Server 提交期拒绝非 AutoBackup 策略携带 TTL，Operator 侧保留只对 AutoBackup 生效的防御行为。

### 3. AppBackup 策略继承

当 `AppBackup.spec.disasterPolicy` 引用的策略类型为 `AutoBackup` 时：

- `appBackup.spec.schedule` 必须继承 `policy.spec.schedule`
- `appBackup.spec.template.ttl` 必须继承 `policy.spec.ttl`
- 若用户直接在 `AppBackup.spec.template.ttl` 中配置了 TTL，策略 TTL 优先，避免“策略选择后实际 TTL 仍来自旧表单”的歧义
- 若策略未配置 TTL，则保持 AppBackup 模板中的 TTL；这兼容既有直接创建 AppBackup 的行为

### 4. 详情回显

Server 与 Web 必须在以下位置回显 TTL：

- `GET /apis/policies.testudo.softcdata.com/v1/policies/:name`
  - `data.spec.ttl`
- `GET /apis/policies.testudo.softcdata.com/v1/policies`
  - 列表项 `spec.ttl`，便于表格展示或调试
- `GET /apis/appbackups.testudo.softcdata.com/v1/appbackups/:name`
  - `data.spec.ttl` 必须显示实际生效 TTL
  - 对使用 AutoBackup 策略的 AppBackup，应优先回显策略继承后的 TTL

历史备份详情继续使用 `status.history[].expiration` 表示每个 Velero Backup 的实际过期时间；本变更不要求反推出每条历史记录的 TTL 字符串。

## Non-Goals

- 不改变 Velero 原生 TTL 语义。
- 不新增独立的 `BackupPolicy` 使用链路。
- 不改变 DataSync/ResourceSync 的调度策略字段。
- 不允许已被 AppBackup 引用的策略绕过现有更新保护；如果现有 Server 逻辑禁止更新被引用策略，本变更保持该保护。
- 不重做自动备份详情页的整体信息架构，仅补齐 TTL 字段。

## Impact

### Operator

- `pkg/apis/disaster/v1/disasterpolicy_types.go`
- `config/crd/bases/testudo.softcdata.com_disasterpolicies.yaml`
- `internal/controller/disasterpolicy_controller.go`
- `internal/controller/appbackup/appbackup_ready.go`
- `internal/controller/appbackup/*_test.go`
- `config/samples/disaster_v1_disasterpolicy.yaml`

### Server

- `/home/chenxi/YS/disaster-server/internal/apis/disaster_policy/v1/types.go`
- `/home/chenxi/YS/disaster-server/internal/apis/disaster_policy/v1/handler.go`
- AppBackup DTO / detail conversion：确保 `spec.ttl` 从 `AppBackup.spec.template.ttl` 或策略继承结果回显
- Apipost 或接口文档示例

### Web

- `/home/chenxi/YS/cluster-disaster-web/src/api/ApiResource/ApiStrategy.ts`
- `/home/chenxi/YS/cluster-disaster-web/src/views/ResourceConfig/StrategyManage/*`
- 自动备份详情页已存在 TTL 展示位，需确保策略模式下详情接口返回有效 `spec.ttl`

### Chart

- 发布更新后的 `DisasterPolicy` CRD。

## Migration

- 新字段为可选字段，存量 `DisasterPolicy` 不需要迁移。
- 存量 `AutoBackup` 策略未配置 TTL 时，保留现有行为。
- 存量 `AppBackup` 已直接配置 `spec.template.ttl` 时，继续按当前值回显；只有在引用的 AutoBackup 策略新增 TTL 后，后续 reconcile 才会将策略 TTL 收敛到 AppBackup 模板与 Velero Schedule。
