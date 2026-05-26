## 1. 实现修复

- [x] 1.1 补充 BDD 测试场景：
  - [x] 场景 A：配置了 DataSyncPolicy 时，DataSync 的 Schedule 应等于策略的 Cron 表达式
  - [x] 场景 B：配置了 ResourceSyncPolicy 时，ResourceSync 的 Schedule 应等于策略的 Cron 表达式
  - [x] 场景 C：策略 State=Disabled 时，Schedule 应为空（不调度）
  - [x] 场景 D：策略引用不存在时，应记录事件并重新入队，不使用硬编码值

- [x] 1.2 修复 `ensureDataSync` 函数：
  - [x] 删除硬编码的 `*/15 * * * *` 默认值
  - [x] 使用 `r.Get` 查询 `DisasterPolicy` CR（名称来自 `config.Spec.DataSyncPolicy`）
  - [x] 读取 `policy.Spec.Schedule` 并赋值给 `dataSync.Spec.Trigger.Schedule`
  - [x] 若策略 `State == Disabled`，清空 Schedule

- [x] 1.3 修复 `ensureResourceSync` 函数：
  - [x] 删除硬编码的 `0 2 * * *` 默认值
  - [x] 使用 `r.Get` 查询 `DisasterPolicy` CR（名称来自 `config.Spec.ResourceSyncPolicy`）
  - [x] 读取 `policy.Spec.Schedule` 并赋值给 `resourceSync.Spec.Trigger.Schedule`
  - [x] 若策略 `State == Disabled`，清空 Schedule

- [x] 1.4 更新 RBAC Marker：
  - [x] 在 `DisasterInstanceReconciler` 上新增 `+kubebuilder:rbac:groups=testudo.softcdata.com,resources=disasterpolicies,verbs=get;list;watch`

- [x] 1.5 错误处理：
  - [x] 若 `DisasterPolicy` 未找到（`IsNotFound`），记录 Warning 事件并返回 `RequeueAfter: 30s`
  - [x] 若其他错误，返回 error 正常重试

## 2. 验证

- [x] 2.1 单元测试通过（16/17，1 个 lifecycle 失败为预存在，与本次修复无关）
- [x] 2.2 `make manifests` 成功，RBAC Role 已更新
- [ ] 2.3 在集群中手动验证：创建 `DisasterPolicy`（不同 Cron），关联到 `DisasterConfig`，创建 `DisasterInstance`，检查 `DataSync.spec.trigger.schedule` 是否正确
