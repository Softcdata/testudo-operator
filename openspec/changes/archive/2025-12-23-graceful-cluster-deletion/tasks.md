# Tasks: 实现优雅删除

- [ ] 在 `pkg/apis/disaster/v1/cluster_types.go` 中：
    - [ ] 添加 `ClusterStatusDeleting` = "Deleting"。
    - [ ] 在 `ClusterStatus` 结构体中添加 `Reason` 和 `Message` 字段。
- [ ] 定义 `AnnotationUninstallVelero` = "testudo.softcdata.com/uninstall-velero"。
- [ ] 在 `ClusterReconciler` 中添加 Finalizer 处理逻辑。
- [ ] 实现 `handleDelete` 函数：
    - [ ] 更新状态为 Deleting。
    - [ ] 调用 `checkDependencies`。
        - [ ] 如果有依赖，更新 `Reason`="DeletionBlocked", `Message`="...", 并 Requeue。
    - [ ] 检查 Annotation 并决定是否调用 `uninstallVelero`。
    - [ ] 移除 Finalizer。
- [ ] 实现 `uninstallVelero` 函数：
    - [ ] 获取目标集群 Client。
    - [ ] 删除 Velero Namespace 或执行 Helm Uninstall。
