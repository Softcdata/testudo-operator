# 变更提案: 修复 Velero 安装检测逻辑 (Fix Velero Detection Logic)

## Why (为什么)

当前 `disaster-operator` 判断集群是否安装了 Velero 的逻辑存在缺陷 (参考 `2025-12-31-improve-velero-detection` 提案)，它仅仅检查 Velero CRD (`BackupList`) 是否可访问。

**问题场景**:
1. **管理集群自纳管**: 管理集群运行 Disaster Operator，为了启动 Operator，必须先安装 Velero CRD (否则 controller-runtime 报错)。
2. **误判**: 当管理集群被注册到 Disaster 平台时，`IsVeleroInstalled` 检测到 CRD 存在，误判为 "Velero 已安装"。
3. **结果**: Operator 跳过了 Velero 服务 (Deployment) 的安装。导致管理集群虽然有 CRD，但没有 Velero 服务运行，处于不可用状态。

**影响**:
- 管理集群无法正常执行备份/恢复任务。
- `checkVeleroVersion` 后续可能会因为找不到 ServerStatusRequest 或者 Service 而报错或卡住。

## What Changes (变更内容)

### 修改 Velero 检测逻辑

修改 `internal/controller/cluster_controller.go` 中的 `IsVeleroInstalled` 方法。

**新逻辑**:
1. **第一步**: 检查 Velero CRD 是否存在 (保持现有逻辑)。如果 CRD 不存在，直接返回 `false` (未安装)。
2. **第二步**: 如果 CRD 存在，进一步检查 `velero` 命名空间下是否存在名为 `velero` 的 Deployment。
3. **判定**: 只有 **CRD 存在** 且 **Deployment 存在**，才返回 `true` (已安装)。

### 代码变更预览

```go
func (r *ClusterReconciler) IsVeleroInstalled(ctx context.Context, cli client.Client) (bool, error) {
    if r.ForceVeleroNotInstalled {
        return false, nil
    }

    // 1. Check CRD availability
    backupList := &velerov1.BackupList{}
    err := cli.List(ctx, backupList, client.Limit(1))
    if err != nil {
        if meta.IsNoMatchError(err) {
            return false, nil // CRD missing
        }
        return false, nil // Other errors
    }

    // 2. Check Deployment existence
    deployment := &appsv1.Deployment{}
    err = cli.Get(ctx, types.NamespacedName{Name: "velero", Namespace: "velero"}, deployment)
    if err != nil {
        if apierrors.IsNotFound(err) {
            return false, nil // Deployment missing (but CRD exists)
        }
        // Permission or network error
        return false, nil
    }

    return true, nil
}
```

## Impact (影响范围)

### 受影响的项目
- **disaster-operator**: Cluster Controller

### 受影响的代码
- `internal/controller/cluster_controller.go`: `IsVeleroInstalled`

### 兼容性
- 向后兼容。对于正常部署的集群（CRD+Deployment），行为不变。
- 对于只有 CRD 的集群（如刚初始化的管理集群），将正确识别为“未安装”并触发安装流程。

## Risks (风险)

- **权限问题**: Operator 需要有读取特定 namespace (`velero`) 下 Deployment 的权限。目前的 ClusterRole 已经是 `apiGroups: ["*"], resources: ["*"]`，权限应当足够。
- **自定义部署名称**: 如果用户手动安装的 Velero Deployment 名字不叫 `velero` 或不在 `velero` namespace，会被误判为未安装并尝试覆盖安装。**假设**: 安装逻辑也是硬编码安装到 `velero/velero`，所以风险可控。
