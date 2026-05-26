# 任务清单：Failover 跳过源集群缩零

## 1. 规范与测试设计（先测后码）
- [x] 1.1 补充/确认 `disaster-operation` 增量规范，明确 `skipScaleDownSource` 的生效范围与兼容行为
- [x] 1.2 设计 BDD 测试场景（正常路径 + 风险路径）
- [ ] 1.3 先实现失败测试（红灯）

## 2. Operator 实现
- [x] 2.1 `DisasterOperationSpec` 增加 `skipScaleDownSource` 字段并更新 CRD 生成产物
- [x] 2.2 在 Failover 的 `ScaleDownSource` 步骤增加显式跳过分支
- [x] 2.3 记录步骤跳过的 Event 与状态消息，保证审计可见
- [x] 2.4 Group 子操作创建时透传 `skipScaleDownSource`

## 3. Operator 测试与验证
- [x] 3.1 新增/更新 failover controller BDD 用例：`skipScaleDownSource=true` 时不执行缩零且流程继续
- [x] 3.2 新增/更新 group operation 用例：父操作参数正确透传到子操作
- [x] 3.3 回归用例：未传参数或 `false` 时保持原有缩零行为
- [ ] 3.4 运行相关测试集并检查核心控制器覆盖率（目标 >= 80%）

## 4. Server 联动（跨仓协同）
- [x] 4.1 在 `disaster-server` 实例操作接口增加并透传 `skipScaleDownSource`
- [x] 4.2 在 `disaster-server` 组操作接口增加并透传 `skipScaleDownSource`
- [x] 4.3 补充 server 侧 handler 测试，验证参数透传到 `DisasterOperationSpec`
- [x] 4.4 更新 API 文档，明确风险提示（可能导致源集群不缩零）

## 5. 质量门禁
- [x] 5.1 运行 `openspec validate add-failover-skip-source-scale-down --strict`
- [x] 5.2 提交评审前核对 proposal/design/spec/tasks 一致性
