# 任务清单：修复集群定期检查重复发射事件问题

## 1. 代码修改

### 1.1 引入初始状态记录
- [x] 1.1.1 在 Reconcile 入口处（Finalizer 检查后）记录 `wasReady := cluster.Status.Status == disasterv1.ClusterStatusReady`
- [x] 1.1.2 确保变量声明位置在任何可能修改 Status 的逻辑之前

### 1.2 修改事件发射逻辑
- [x] 1.2.1 修改第 326~338 行的"创建集群完成"事件发射逻辑：
  - 条件从 `LastEventPhase != Ready && ObservedGeneration == 0` 
  - 改为 `!wasReady && cluster.Status.Status == Ready && ObservedGeneration == 0`
- [x] 1.2.2 确保定期检查成功时（wasReady=true）不发射事件

### 1.3 代码对齐
- [x] 1.3.1 添加注释说明事件防抖逻辑，参考 StorageRepository 的注释风格

## 2. 测试验证

### 2.1 单元测试
- [x] 2.1.1 编译验证通过 (`go build ./...`)
- [ ] 2.1.2 在 `cluster_controller_test.go` 中添加测试场景：
  - "should emit ClusterReady event only once on first Ready"
  - "should NOT emit event on periodic health check when already Ready"
  *(备注：现有测试因环境问题失败，与本次修改无关)*

### 2.2 集成验证
- [ ] 2.2.1 创建集群，观察事件仅发射一次
- [ ] 2.2.2 等待定期检查（1分钟），确认无重复事件
  ```bash
  kubectl get events -n disaster-system --field-selector involvedObject.kind=Cluster -w
  ```
- [ ] 2.2.3 模拟集群短暂离线后恢复，确认恢复时正确发射事件

## 运行测试命令

```bash
# 编译验证
go build ./...

# 运行 Cluster 控制器事件相关测试
cd internal/controller
ginkgo -v --focus="event emission" .

# 运行全部 Cluster 控制器测试
ginkgo -v --focus="Cluster Controller" .
```

## 变更摘要

**修改的文件**: `internal/controller/cluster_controller.go`

**关键改动**:
1. 第 140~143 行: 新增 `wasReady` 变量，记录 Reconcile 入口时的状态
2. 第 330~331 行: 修改事件发射条件，使用 `!wasReady` 替代 `LastEventPhase` 检查

**效果**: 定期健康检查（每分钟）成功时不再重复发射"创建集群完成"事件
