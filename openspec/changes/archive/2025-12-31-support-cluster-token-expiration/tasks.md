# 任务列表：支持集群 Token 过期时间检测

- [x] 更新 `Cluster` CRD (`pkg/apis/disaster/v1/cluster_types.go`)，在 `ClusterStatus` 中添加 `TokenExpiration *metav1.Time` 字段。 <!-- id: update-crd -->
- [x] 运行 `make manifests` 和 `make generate` 以更新 CRD 定义和 DeepCopy 方法。 <!-- id: generate-manifests -->
- [x] 在 `pkg/helper` 或 `internal/controller/cluster` 中添加 JWT 解析辅助函数。 <!-- id: add-jwt-helper -->
- [x] 更新 `Cluster` Reconciler，当 `Spec.Token` 存在时调用 JWT 解析器。 <!-- id: update-controller -->
- [x] 更新 `Cluster` Reconciler，根据解析出的 `exp` claim 设置 `Status.TokenExpiration`。 <!-- id: update-status -->
- [x] 更新 `Cluster` Reconciler，当 Token 过期或鉴权失败时，填充 `Status.Reason` 和 `Status.Message`。 <!-- id: update-reason-message -->
- [x] 为 JWT 解析和状态更新逻辑添加单元测试。 <!-- id: add-tests -->
- [x] 在 `disaster-server` 中实现 `PATCH /api/v1/clusters/:id` 接口，仅允许更新 Token 和 Tags。 <!-- id: server-update-api -->
- [x] 添加服务端单元测试确保除 Token/Tags 外的字段不可被修改。 <!-- id: server-api-test -->
