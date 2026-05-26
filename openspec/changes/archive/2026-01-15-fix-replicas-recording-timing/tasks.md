# 实现任务 (Implementation Tasks)

- [x] **1. 修改 DisasterOperation 控制器**
    - [x] 在 `executeScaleDownSource` 中，缩容之前先调用 `recordReplicasBeforeScaleDown`
    - [x] 实现 `recordReplicasBeforeScaleDown` 函数:
        - [x] 扫描源集群 Deployments 和 StatefulSets
        - [x] 记录当前副本数到 ConfigMap (`replicas-{ResourceSyncName}`)
        - [x] 仅记录副本数 > 0 的资源
    - [x] 添加错误处理和事件记录

- [x] **2. 修改 ResourceSync 控制器 (兼容性)**
    - [x] 修改 `recordReplicasToConfigMap`:
        - [x] 检查现有 ConfigMap 中的副本数
        - [x] 如果当前扫描到的副本数为0，但 ConfigMap 中有非零记录，则保留原记录不覆盖
        - [x] 这避免 Failover 场景下覆盖正确的副本数

- [x] **3. 验证**
    - [x] 编译验证: `go build ./...` 通过
    - [x] E2E 测试: V2 Failover 流程
    - [x] 验证目标集群 StatefulSet 副本数恢复正确
