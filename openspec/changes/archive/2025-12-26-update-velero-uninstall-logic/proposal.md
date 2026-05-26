# Proposal: 使用 Helm 卸载 Velero

## Summary
修改 `ClusterReconciler` 中的 Velero 卸载逻辑，使用 `helm uninstall` 代替直接删除命名空间。这确保了与安装方法（Helm）的一致性，并降低了留下孤立资源或清理不当的风险。

## Motivation
目前，`uninstallVelero` 函数直接删除 `velero` 命名空间。这种方法存在风险，因为：
1. 它绕过了 Helm 的发布管理，即使资源已经消失，Helm release 仍处于“已部署”状态。
2. 如果集群范围的资源由 Helm 管理但不在命名空间中，它可能无法正确清理。
3. 这与使用 Helm 的安装过程不一致。

## Proposed Solution
- 修改 `ClusterReconciler.uninstallVelero` 以执行 `helm uninstall -n velero velero`。
- 确保在此操作中使用 `CommandExecutor` 以支持测试（mocking）。
- 处理 Helm release 可能不存在的情况。
- **测试规划**：在 `cluster_controller_test.go` 中添加专门的测试用例，模拟带有 `AnnotationUninstallVelero` 注解的集群删除场景，并断言 `helm uninstall` 命令被正确调用。

## Alternatives Considered
- **保持当前行为**：由于上述风险而被拒绝。
- **使用 Velero CLI**：`velero uninstall`。这也是一个选项，但由于我们使用 Helm 安装，使用 Helm 卸载更加对称。

## Risks
- 如果运行 operator 的环境中没有 Helm，这将失败。（然而，安装已经需要 Helm，所以这是一个低风险）。
- 如果 release 已经消失但命名空间存在，`helm uninstall` 可能会失败。我们需要处理幂等性。
