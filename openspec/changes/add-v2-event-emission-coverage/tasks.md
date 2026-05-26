## 1. 规范与设计

- [x] 1.1 完成 `proposal.md`，明确 V2 事件覆盖目标、边界与风险
- [x] 1.2 完成 `design.md`，产出六大模块事件发射矩阵（Started/Progress/Finished）
- [x] 1.3 提交 `specs/global-events/spec.md` 增量规范，明确运行期方向语义
- [x] 1.4 输出 `execution-plan.md`，明确实施顺序、模块边界与风险控制
- [x] 1.5 输出 `e2e-test-procedure.md`，提供真实环境可执行的验证步骤

## 2. 测试先行（BDD）

- [ ] 2.1 为 `DisasterInstance` 补充事件测试：创建、初始化推进、失败终态、删除
- [ ] 2.2 为 `DataSync` 补充事件测试：调度触发、关键步骤进度、成功/失败
- [ ] 2.3 为 `ResourceSync` 补充事件测试：触发、步骤推进、成功/失败
- [x] 2.4 为 `DisasterOperation` 补充事件测试：failover/reprotect/sync 的开始、步骤、终态
- [ ] 2.5 为 `DisasterGroup` 补充事件测试：按 Level 推进进度与汇总终态
- [ ] 2.6 为 `DisasterDrill` 补充事件测试：确认执行、过程进度、完成/失败、cleanup
- [ ] 2.7 补充反向路径测试：failover 后 reprotect + 反向 sync 事件方向一致性
- [ ] 2.8 补充错误路径测试：步骤超时、步骤失败、删除清理失败的 Finished 事件收敛

## 3. Operator 实施

- [x] 3.1 在六大 V2 控制器中统一接入 `ReportTaskStartedWithClient/ProgressWithClient/FinishedWithClient`
- [x] 3.2 统一任务命名与消息语义，避免同类动作多种命名
- [ ] 3.3 对多步骤流程增加阶段去重，避免重复 Progress 事件
- [x] 3.4 在删除路径保证先发 Finished 再移除 Finalizer
- [x] 3.5 对涉及主备方向的任务，改为基于运行期角色计算 source/target

## 4. 跨仓联调验证

- [ ] 4.1 验证 `disaster-server` `/apis/v1/events` 可聚合 V2 新事件
- [ ] 4.2 验证 `disaster-server` watch 流可连续输出 V2 事件进度
- [ ] 4.3 验证 `cluster-disaster-web` 对 V2 事件通知和历史展示完整可读

## 5. 验证与发布准备

- [x] 5.1 执行 `openspec validate add-v2-event-emission-coverage --strict`
- [ ] 5.2 补充变更说明（迁移策略、兼容说明、回滚注意事项）
- [ ] 5.3 完成提案评审并进入实施阶段（/openspec-apply）
