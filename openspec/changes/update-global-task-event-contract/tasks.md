## 1. Spec 与设计
- [ ] 1.1 在 `global-events` 增量规范中补 durable history 发射契约
- [ ] 1.2 明确本 change 与 `add-v2-event-emission-coverage`、`update-global-events-end-to-end-noise-control` 的边界

## 2. Event Reporter 约束
- [ ] 2.1 锁定 durable history 所依赖的最小稳定字段集
- [ ] 2.2 明确无 trace / cluster / user 时的占位值策略
- [ ] 2.3 明确 `Source.Component=disaster-operator` 与带 label 结构化事件是 history 主链路前提

## 3. Controller 审计与迁移
- [ ] 3.1 审计 `disasterinstance` / `datasync` / `resourcesync` / `disasteroperation` / `disasterdrill` / `cluster` 的结构化事件路径
- [ ] 3.2 对仍仅依赖旧 `Recorder.Event*` 的 history 主链路补齐 `WithClient` 事件
- [ ] 3.3 补齐删除路径、失败路径、补偿路径的 `ExecutionFinished` 终态覆盖

## 4. Verification
- [ ] 4.1 补充 `event_reporter.go` 单测，覆盖 stable trace/default placeholder/source component
- [ ] 4.2 补充控制器回归验证：同一次执行 Started/Progress/Finished 不发生 trace 漂移
- [ ] 4.3 与 `disaster-server` 联调 durable history 聚合结果
