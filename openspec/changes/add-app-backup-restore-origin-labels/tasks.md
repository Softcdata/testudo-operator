# 任务清单：来源标签 + 列表默认过滤 + 事件流收敛

## 1. 规范与测试设计（先测后码）
- [ ] 1.1 更新 `app-backup-lifecycle`、`app-restore`、`global-events` 增量规范，明确资源来源与事件来源标签语义
- [ ] 1.2 设计 BDD 场景：用户创建、DataSync 创建、ResourceSync 创建、存量回填、事件流过滤
- [ ] 1.3 先实现失败测试（红灯）并确认差异可稳定复现

## 2. Operator 实现
- [ ] 2.1 在 `pkg/metadata/labels.go` 增加资源来源标签常量与事件来源标签常量
- [ ] 2.2 在 `AppBackup` 控制器 `syncLabels` 中按 OwnerReference 维护来源标签
- [ ] 2.3 在 `AppRestore` 控制器 `syncLabels` 中按 OwnerReference 维护来源标签
- [ ] 2.4 在 `DataSync` / `ResourceSync` 创建 `AppBackup` 与 `AppRestore` 时设置来源标签（即时可见）
- [ ] 2.5 扩展 `pkg/helper/event_reporter.go`，发射结构化事件时支持附加 `task-origin` / `task-origin-kind` 标签
- [ ] 2.6 在 `AppBackup` / `AppRestore` 事件上报调用链中透传事件来源标签值
- [ ] 2.7 确保来源标签与现有搜索标签、依赖标签并存，不互相覆盖

## 3. Operator 测试与验证
- [ ] 3.1 AppBackup 场景测试：DataSync/ResourceSync/用户来源三种标签判定
- [ ] 3.2 AppRestore 场景测试：DataSync/ResourceSync/用户来源三种标签判定
- [ ] 3.3 存量回填测试：缺少来源标签的旧资源在 Reconcile 后被补齐
- [ ] 3.4 事件标签测试：结构化事件必须携带正确 `task-origin` / `task-origin-kind`
- [ ] 3.5 回归测试：现有 `app-backup-type`、`app-restore-source-type`、状态标签逻辑不回归
- [ ] 3.6 运行相关测试集并检查核心控制器覆盖率（目标 >= 80%）

## 4. Server 联动（跨仓协同）
- [ ] 4.1 `GET /apis/v1/appbackups` 增加 `origin` 参数并设置默认 `origin=user`
- [ ] 4.2 `GET /apis/v1/apprestores` 增加 `origin` 参数并设置默认 `origin=user`
- [ ] 4.3 `GET /apis/v1/watch/events` 按 `task-origin` 默认过滤系统同步任务，仅推送用户任务
- [ ] 4.4 `watch/events` 支持 `origin=all` 显式获取全量结构化事件
- [ ] 4.5 补充 server 侧 handler 单元测试：默认过滤、`origin=all`、`origin=instance`
- [ ] 4.6 更新 API 文档，明确默认过滤行为与查询参数

## 5. 质量门禁
- [ ] 5.1 运行 `openspec validate add-app-backup-restore-origin-labels --strict`
- [ ] 5.2 评审前核对 `proposal/design/spec/tasks` 一致性
