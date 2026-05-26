# Proposal: 为集群协调添加 Token 认证支持

## Summary
更新 Cluster Controller 以支持使用 Token 和 Endpoint 连接到受管集群。

## Motivation
`Cluster` CRD 将新增 `Endpoint` 字段配合 `Token` 使用。当前的控制器逻辑仅依赖 `KubeConfig`。为了完全支持 CRD 定义并提供灵活性，控制器必须能够使用提供的 Token 和 Endpoint 进行认证。

## Proposed Changes

### 1. 工具库扩展 (`pkg/tools`)
- 在 `pkg/tools/kubeconfig.go` (或新文件) 中添加辅助函数：
  ```go
  func GetRestConfigFromToken(endpoint, token string) (*rest.Config, error) {
      return &rest.Config{
          Host:            endpoint,
          BearerToken:     token,
          TLSClientConfig: rest.TLSClientConfig{Insecure: true},
      }, nil
  }
  ```

### 2. Controller 修改 (`internal/controller/cluster_controller.go`)
- 修改 `ClusterReconciler.Reconcile` 方法：
    - **连接逻辑更新**：
        1. 检查 `cluster.Spec.KubeConfig`。如果存在，继续使用 `tools.GetRestConfig`。
        2. 如果 `KubeConfig` 不存在，检查 `cluster.Spec.Token` 和 `cluster.Spec.Endpoint`。
        3. 如果两者都存在，调用 `tools.GetRestConfigFromToken(cluster.Spec.Endpoint, cluster.Spec.Token)` 获取配置。
        4. 如果都缺失，返回错误或忽略（视状态而定）。
    - **状态更新**：
        - 确保 `cluster.Status.Endpoint` 正确反映使用的 Endpoint (如果是 Token 方式，直接使用 Spec 中的；如果是 KubeConfig，从 Config 中提取)。

### 4. 通用工具修改 (`internal/controller/common.go`)
- 修改 `GetKubeClientSetWithCluster` 函数：
    - 增加对 `Token` 和 `Endpoint` 的检查。
    - 如果 `KubeConfig` 为空，尝试使用 `tools.GetRestConfigFromToken` 创建配置。
    - 确保所有依赖此函数的控制器（如 `DisasterBackupReconciler`）都能正确处理 Token 认证。

### 5. DisasterBackup Controller 修改 (`internal/controller/disasterbackup_controller.go`)
- 将 `getKubeConfigByClusterName` 重构为 `getRestConfigByClusterName`。
- 直接返回 `*rest.Config` 而不是 `[]byte` (kubeconfig content)。
- 在函数内部处理 Token/Endpoint 逻辑，确保在没有 KubeConfig 时也能返回有效的 REST 配置。
- 更新调用处以使用返回的 `rest.Config` 创建 Discovery Client。

### 6. Event Recording
- 在 `ClusterReconciler` 中添加 `EventRecorder`。
- 在关键步骤（如配置获取失败、连接失败）记录 Kubernetes Events，以便用户排查问题。
