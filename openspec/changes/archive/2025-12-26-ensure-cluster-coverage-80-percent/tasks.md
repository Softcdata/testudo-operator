# Tasks: 确保 Cluster 模块测试覆盖率 > 80%

- [x] **错误处理场景** <!-- id: 0 -->
    - [x] 测试 `GetRestConfig` 错误（无效的 kubeconfig 格式）。 <!-- id: 1 -->
    - [x] 测试 `GetRestConfigFromToken` 错误（无效的 token/endpoint）。 <!-- id: 2 -->
    - [x] 测试同时缺少 KubeConfig 和 Token (InvalidSpec)。 <!-- id: 3 -->
    - [x] 测试 `kubernetes.NewForConfig` 错误。 <!-- id: 4 -->
    - [x] 测试 `client.New` 错误。 <!-- id: 5 -->
    - [x] 测试 `IsVeleroInstalled` 错误。 <!-- id: 6 -->
    - [x] 测试 `InstallVeleroInCluster` 错误。 <!-- id: 7 -->
    - [x] 测试 `ServerVersion` 发现错误。 <!-- id: 8 -->

- [x] **删除逻辑** <!-- id: 9 -->
    - [x] 测试存在依赖时的删除（应阻塞）。 <!-- id: 10 -->
    - [x] 测试删除成功（移除 finalizer）。 <!-- id: 11 -->

- [x] **验证** <!-- id: 12 -->
    - [x] 运行测试并验证覆盖率 > 80%。 <!-- id: 13 -->
