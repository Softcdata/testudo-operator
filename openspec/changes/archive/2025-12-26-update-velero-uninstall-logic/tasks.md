# Tasks: 使用 Helm 卸载 Velero

- [x] 修改 `internal/controller/cluster_controller.go` 中的 `ClusterReconciler.uninstallVelero` <!-- id: impl-uninstall -->
    - [x] 使用 `r.CommandExecutor.Run` 执行 `helm uninstall`。
    - [x] 处理错误（例如，release 未找到）。
- [x] 更新 `internal/controller/cluster_controller_test.go` 中的单元测试 <!-- id: test-uninstall -->
    - [x] 更新 `MockCommandExecutor` 的使用预期。
    - [x] 添加新的测试用例：`should uninstall velero using helm when annotation is present`。
    - [x] 验证在使用 `AnnotationUninstallVelero` 删除时是否调用了 `helm uninstall`。
