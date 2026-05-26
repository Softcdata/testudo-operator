# 任务清单：Cluster 与 StorageRepository 结构化事件发射 (Operator)

## 1. Cluster 控制器事件

### 1.1 创建与就绪阶段事件
- [x] 1.1.1 在资源首次创建时（Finalizer 添加后）发射 `ClusterCreated` 事件
- [x] 1.1.3 在集群完全就绪后（连接成功 + Velero 可用）发射 `ClusterReady` 事件

### 1.2 Velero 安装阶段事件
- [x] 1.2.1 在调用 `InstallVeleroInCluster` 前发射 `VeleroInstalling` 事件
- [x] 1.2.2 在 Velero 安装成功后发射 `VeleroInstalled` 事件
- [x] 1.2.3 在 Velero 安装失败时发射 `VeleroInstallFailed` 事件 (携带错误信息)

### 1.3 删除阶段事件
- [x] 1.3.1 在 `handleDelete` 开始时发射 `ClusterDeleting` 事件

## 2. StorageRepository 控制器事件

### 2.1 创建与就绪阶段事件
- [x] 2.1.1 确认 StorageRepository 有独立控制器 (`storagerepository_controller.go`)
- [x] 2.1.2 在资源首次创建时（Finalizer 添加后）发射 `StorageCreated` 事件
- [x] 2.1.3 在验证成功后发射 `StorageReady` 事件
- [x] 2.1.4 在验证失败时发射 `StorageUnavailable` 事件 (携带错误信息)

## 3. 复用现有 EventReporter

- [x] 3.1 确认 `pkg/helper/event_reporter.go` 的函数签名可直接复用
- [x] 3.2 确保 `taskName` 格式符合规范：`Cluster: {name}` 或 `Storage: {name}`

## 4. 避免重复发射事件 (关键)

- [x] 4.1 **Cluster**: 在 Status 中增加 `LastEventPhase` 字段
- [x] 4.2 **StorageRepository**: 在 Status 中增加 `LastEventPhase` 字段
- [x] 4.3 在发射事件前检查 `status.LastEventPhase != currentPhase`
- [x] 4.4 参考 `appbackup_ready.go:456` 的状态变更检测逻辑

## 5. Duration 计算

- [x] 5.1 **Cluster**: 使用 `metadata.creationTimestamp` 作为 StartTime
- [x] 5.2 **Cluster**: 在 Status 中增加 `ReadyTimestamp` 字段
- [x] 5.3 **StorageRepository**: 同上，增加 `ReadyTimestamp` 字段
- [x] 5.4 调用 `helper.CalculateDuration(startTime, endTime)` 计算耗时

## 6. 单元测试

- [x] 6.1 在 `cluster_controller_test.go` 中添加事件发射测试 Context
- [x] 6.2 测试 `ClusterCreated` 事件在 Finalizer 添加后发射
- [x] 6.3 测试 `ClusterReady` 事件只发射一次（通过 `LastEventPhase` 验证）

### 运行测试命令
```bash
# 使用 ginkgo 运行事件相关测试
cd internal/controller
ginkgo -v --focus="event emission" .

# 或运行全部 Cluster 控制器测试
ginkgo -v --focus="Cluster Controller" .

# 运行单个测试用例
ginkgo -v --focus="ClusterCreated event" .
```

## 7. 集成验证

- [ ] 7.1 创建集群，观察事件输出
  ```bash
  kubectl get events -n disaster-system --field-selector involvedObject.kind=Cluster
  # 确认事件带有 Label: testudo.softcdata.com/task-event=true
  ```
- [ ] 7.2 多次触发 Reconcile，确认不会重复发射相同状态的事件
- [ ] 7.3 删除集群，观察 `ClusterDeleting` 事件
- [ ] 7.4 创建存储，观察 `StorageCreated` → `StorageReady` 事件流
- [ ] 7.5 验证 Duration 字段格式正确（如 `45s`, `2m30s`）

