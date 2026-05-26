# Tasks: AutoBackup 策略更新传播

## 1. Proposal

- [x] 1.1 明确 AutoBackup 与 DataSync/ResourceSync 策略链路分离。
- [x] 1.2 明确验收口径以 AppBackup 与 Velero Schedule 实际状态为准。
- [x] 1.3 明确策略更新传播由 AppBackup 控制器承担。

## 2. Operator

- [x] 2.1 在 `AppBackupReconciler` 中 watch `DisasterPolicy` 变更。
- [x] 2.2 为 `DisasterPolicy(type=AutoBackup)` 变更映射并 enqueue 同 namespace 下引用该策略的 `AppBackup`。
- [x] 2.3 确认 `DataSync` / `ResourceSync` 类型策略不会触发 AutoBackup AppBackup 入队。
- [x] 2.4 将 AutoBackup policy 的 `schedule`、`ttl`、`state` 计算为 AppBackup 的 effective 调度配置。
- [x] 2.5 当 AppBackup 引用 AutoBackup policy 时，策略 schedule/ttl 优先于 AppBackup 表单残留值。
- [x] 2.6 在 policy `state=Disabled` 时暂停 Velero Schedule，避免继续产生新 Backup。
- [x] 2.7 将 `CreateVeleroSchedule` 升级为 ensure/update 语义，已存在 Schedule 时比较并更新 `spec.schedule/spec.template.ttl/spec.paused/spec.template.storageLocation` 等有效字段。
- [x] 2.8 当 Velero Schedule 无法原地更新时，安全删除并重建 Schedule，不删除历史 Backup。
- [x] 2.9 记录策略传播事件与失败原因，便于 Server/UI 展示。

## 3. Server / Web Alignment

- [x] 3.1 Server 删除/引用判断中将 AutoBackup 策略与容灾同步策略分离。
- [x] 3.2 Server 自动备份详情回显实际生效 schedule/ttl/paused。
- [x] 3.3 Server 新增自动备份执行统计接口，支持 `period=7d|30d|90d`（兼容 `range`）。
- [x] 3.4 自动备份统计接口只统计 AutoBackup 链路成功/失败历史，不混入容灾同步备份或手动备份。
- [x] 3.5 自动备份统计接口返回总数、成功数、失败数、成功百分比、失败百分比和窗口 start/end。
- [ ] 3.6 Web 编辑 AutoBackup 策略后提示其会收敛到引用它的自动备份调度。
- [ ] 3.7 Web 不使用 DisasterConfig/DisasterInstance 引用关系判断 AutoBackup 策略是否“未使用”。
- [ ] 3.8 Web 首页“计划备份执行情况”图表接入自动备份统计接口，支持 7 天、30 天、90 天切换。

## 4. Tests

- [x] 4.1 单测：AutoBackup policy schedule/ttl 更新会入队引用它的 AppBackup。
- [x] 4.2 单测：非 AutoBackup policy 更新不会入队 AppBackup。
- [x] 4.3 单测：引用 policy 的 AppBackup effective schedule/ttl 来自 policy。
- [x] 4.4 单测：已存在 Velero Schedule 的 schedule/ttl 不一致时被更新。
- [x] 4.5 单测：policy Disabled 后 Velero Schedule paused。
- [ ] 4.6 单测：无法原地更新 Schedule 时执行安全重建。
- [x] 4.7 Server 单测：自动备份统计接口按 7d/30d/90d 过滤。
- [x] 4.8 Server 单测：自动备份统计接口只统计 AutoBackup，不统计 DataSync/ResourceSync 和手动备份。
- [x] 4.9 Server 单测：统计接口成功/失败百分比计算正确，空数据返回 0。

## 5. Verification

- [x] 5.1 `openspec validate update-autobackup-policy-schedule-propagation --strict`。
- [ ] 5.2 `go test ./internal/controller/appbackup -count=1`。
- [x] 5.3 `go test ./internal/controller -run DisasterPolicy -count=1` 或对应 Ginkgo 定向测试。
- [ ] 5.4 E2E：创建 AutoBackup policy + AppBackup，修改 policy schedule/ttl，验证 Velero Schedule 实际更新。
- [ ] 5.5 E2E：禁用 AutoBackup policy，验证 Velero Schedule paused 且不新增 Backup。
- [ ] 5.6 E2E：自动备份成功/失败历史聚合后，7 天、30 天、90 天统计接口返回正确数量和百分比。
- [ ] 5.7 Web smoke test：首页图表展示统计接口返回的自动备份成功/失败数据。
