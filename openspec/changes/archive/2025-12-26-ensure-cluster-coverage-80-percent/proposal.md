# 确保 Cluster 模块测试覆盖率 > 80%

## Summary
本提案旨在将 `Cluster` 模块（特别是 `ClusterReconciler`）的单元测试覆盖率提高到 80% 以上。重点是为目前未覆盖的错误处理路径和边缘情况添加测试用例。

## Motivation
当前的覆盖率分析显示 `ClusterReconciler` 中的错误处理逻辑存在显著空白。为了确保系统稳定性与可靠性，特别是在故障场景下，我们需要覆盖这些路径。目标是使集群模块的代码覆盖率达到 >80%。

## Proposed Changes
1.  **扩展 `ClusterReconciler` 测试**: 在 `internal/controller/cluster_controller_test.go` 中添加特定的测试用例以覆盖：
    -   无效的 KubeConfig 或 Token 场景。
    -   Client 创建失败。
    -   Velero 安装检查失败。
    -   Velero 安装执行失败。
    -   Cluster 版本检查失败。
    -   删除逻辑和依赖检查。

## Implementation Plan
1.  分析 `coverage.out` 以识别具体的未覆盖代码块。
2.  使用 `envtest` 和 Mock 实现测试用例以模拟错误条件。
3.  验证覆盖率达到目标 >80%。

## Testing Strategy
-   使用现有的 `envtest` 设置。
-   在必要时 Mock `tools` 函数或 `client` 接口以注入错误。
