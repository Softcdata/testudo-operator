# Tasks: Verify Velero Installation Completeness

- [x] **Spec Update**: 更新 Cluster 规范，要求 Cluster 状态 Ready 的前提是 Velero Deployment 和 Node-Agent DaemonSet 均已就绪。 <!-- id: spec-update -->
- [x] **Controller Logic**: 更新 `ClusterReconciler.IsVeleroInstalled` 逻辑，增加对 `node-agent` DaemonSet 的检查。 <!-- id: controller-logic -->
- [x] **Check Logic**: 确保只有当 Deployment 和 DaemonSet 都存在（且可选的 Ready）时，才返回 true。 <!-- id: check-logic -->
- [x] **Logging**: 如果 `node-agent` 缺失，在日志或 Event 中明确指出。 <!-- id: logging -->
