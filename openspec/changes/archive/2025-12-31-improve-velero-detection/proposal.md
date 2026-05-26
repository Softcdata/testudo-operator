# 变更提案: 改进 Velero 安装检测逻辑

## Why (为什么)

当前 Cluster Controller 的 `IsVeleroInstalled` 方法存在缺陷:

**当前实现**:
```go
func (r *ClusterReconciler) IsVeleroInstalled(ctx context.Context, cli client.Client) (bool, error) {
    velero := &appsv1.Deployment{}
    err := cli.Get(ctx, client.ObjectKey{Name: "velero", Namespace: "velero"}, velero)
    if err != nil {
        if apierrors.IsNotFound(err) || strings.Contains(err.Error(), "not found") {
            return false, nil // Velero未安装
        }
        return false, err // 其他错误
    }
    return true, nil // Velero已安装
}
```

**问题**:
1. **仅检查 Deployment 存在性**: 只检查 `velero` namespace 中是否有 `velero` Deployment
2. **误判风险高**: 
   - 如果 Velero Deployment 被删除但 CRD 仍存在,会误判为"未安装"并尝试重新安装
   - 如果 namespace 不存在,会误判为"未安装"
   - 无法检测 Velero 是否真正可用(CRD 是否就绪)

**实际影响**:
- 在 E2E 测试中,如果 Velero 已安装但 Deployment 暂时不可用,会触发重复安装
- 集群状态判断不准确,导致 AppBackup 创建时机错误

## What Changes (变更内容)

### 改进 Velero 检测逻辑

**新实现方案**: 检查 Velero CRD 是否存在且可访问

```go
func (r *ClusterReconciler) IsVeleroInstalled(ctx context.Context, cli client.Client) (bool, error) {
    // 方案1: 检查关键 CRD 是否可访问
    backupList := &velerov1.BackupList{}
    err := cli.List(ctx, backupList, client.Limit(1))
    if err != nil {
        if meta.IsNoMatchError(err) {
            return false, nil // Velero CRD 不存在,未安装
        }
        // 其他错误(如权限问题)也视为未安装
        return false, nil
    }
    return true, nil // CRD 可访问,Velero 已安装
}
```

**优势**:
1. **更准确**: CRD 存在是 Velero 安装的核心标志
2. **更可靠**: 即使 Deployment 暂时不可用,CRD 仍然存在
3. **与删除保护逻辑一致**: 复用了 `fix-resource-dependency-validation` 提案中的 CRD 检测模式

## Impact (影响范围)

### 受影响的项目
- **disaster-operator**: Cluster Controller 的 Velero 检测逻辑

### 受影响的代码
- `internal/controller/cluster_controller.go` - `IsVeleroInstalled` 方法

### 破坏性变更
- **无破坏性变更**: 仅改进检测逻辑,不改变外部行为

## 技术细节

### 检测逻辑对比

| 检测方式 | 当前方案 | 新方案 |
|---------|---------|--------|
| **检测目标** | Velero Deployment | Velero CRD (Backup) |
| **检测方法** | `cli.Get(Deployment)` | `cli.List(BackupList, Limit(1))` |
| **误判风险** | 高 (Deployment 可能暂时不可用) | 低 (CRD 是持久化资源) |
| **与其他逻辑一致性** | 低 | 高 (与删除保护逻辑一致) |

### 实现代码

```go
// IsVeleroInstalled checks if Velero is installed by verifying CRD availability
func (r *ClusterReconciler) IsVeleroInstalled(ctx context.Context, cli client.Client) (bool, error) {
    if r.ForceVeleroNotInstalled {
        return false, nil
    }
    
    // Check if Velero Backup CRD is available
    backupList := &velerov1.BackupList{}
    err := cli.List(ctx, backupList, client.Limit(1))
    if err != nil {
        if meta.IsNoMatchError(err) {
            // CRD not found, Velero not installed
            return false, nil
        }
        // Other errors (permission, connection) also indicate Velero is not properly installed
        return false, nil
    }
    
    // CRD is accessible, Velero is installed
    return true, nil
}
```

### 需要导入的包

```go
import (
    "k8s.io/apimachinery/pkg/api/meta"
)
```

## 风险与缓解

### 风险1: CRD 存在但 Velero 服务不可用
- **场景**: CRD 已安装但 Velero Deployment 被删除
- **缓解**: 
  - 后续的 `checkVeleroVersion` 方法会通过 ServerStatusRequest 验证 Velero 服务是否可用
  - 如果服务不可用,集群状态会被设置为 NotReady

### 风险2: 权限不足导致误判
- **场景**: 客户端没有 List Backup 的权限
- **缓解**: 
  - 当前实现将权限错误视为"未安装",这是保守策略
  - 可以在日志中记录详细错误信息,便于排查

## 测试计划

1. **单元测试**: 模拟 CRD 存在/不存在的场景
2. **集成测试**: 在真实集群中验证检测逻辑
3. **E2E 测试**: 验证集群添加流程在 Velero 已安装场景下的行为
