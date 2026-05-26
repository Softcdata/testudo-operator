---
description: Manually manage Velero CRDs installation in Go code to bypass flaky Helm Hook/Job image dependencies.
---

# Proposal: 手动安装 Velero CRDs (Manual Installation of Velero CRDs)

## Context (背景)
使用 `helm install` 并启用 hooks 依赖于一个临时的 `velero-upgrade-crds` Job。这个 Job 需要一个 `kubectl` 容器镜像，该镜像历史上导致了严重的兼容性问题（glibc 版本不匹配、架构不兼容）和网络问题（代理/镜像站 400 错误、tag 未找到）。
与其无休止地调试 Job 镜像，我们建议将 CRD 安装的责任从 Helm Hook Job 转移到 Operator 的 Go 代码中。

## Goals (目标)
1. **消除不稳定依赖 (Eliminate Flaky Dependencies)**: 移除对 `kubectl` 镜像和 Helm Hook Job 进行 CRD 安装的依赖。
2. **健壮的安装 (Robust Installation)**: 使用 Operator 的 Go client 直接安装 CRDs，这是架构无关的，且不需要外部网络。
3. **更快的 Reconcile (Faster Reconcile)**: 减少等待 Pod 调度和镜像拉取的时间。

## Solution (方案)

### 1. 资源管理 (Asset Management)
- **非嵌入式**: 不使用 Go `embed`，而是通过 Dockerfile 将 `dist/velero-crds.yaml` 复制到镜像中的固定路径（如 `/app/velero-crds.yaml`）。
- **运行时读取**: Operator 代码在运行时通过 `os.ReadFile` 读取该路径下的 CRD 文件。
- 确保 `dist/velero-crds.yaml` 包含所有必要的 Velero CRDs。

### 2. 控制器逻辑更新 (`internal/controller/cluster_controller.go`)
- **Before** 运行 `helm upgrade`:
    - 调用一个新函数 `EnsureVeleroCRDs(ctx, destClient)`.
    - 该函数解析嵌入的 CRD YAML 并将每个 CRD 应用到目标集群。
    - 使用 Server-Side Apply (SSA) 或 Create-or-Patch 逻辑更新现有的 CRDs。
- **During** `helm upgrade`:
    - 恢复 `--no-hooks` 标志以跳过 `velero-upgrade-crds` Job 的执行。
    - (可选) 或者设置 Helm values 以禁用 CRD job，如果 chart 支持的话（例如 `upgradeCRDs: false`），但针对此特定问题，`--no-hooks` 更干净。

### 3. 实施步骤 (Implementation Steps)
1.  **嵌入 CRDs**: 创建一个包（例如 `pkg/velero/crds`）来保存 `velero-crds.yaml` 并暴露 Accessor。
2.  **逻辑**: 使用 `controller-runtime` client（或 dynamic client）实现 `ApplyCRDs`。
3.  **集成**: 在 `InstallVeleroInCluster` 内部调用 `ApplyCRDs`.
4.  **清理**: 更新 Helm 命令参数。

## Verification (验证)
- 单元测试: Mock client 以验证 CRD 创建调用。
- E2E: 验证 Velero 安装成功且没有出现 `upgrade-crds` pod。

## Impact (影响范围)
- **Modules**: `ClusterReconciler`
- **Files**: `internal/controller/cluster_controller.go`, 新增 `pkg/velero/crds.yaml`
