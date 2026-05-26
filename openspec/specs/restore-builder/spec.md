# AppRestore 构建器规范

## 1. 概述

`restore/builder.go` 提供了统一的 `AppRestore` 构建接口，供 `ResourceSync`、`DataSync` 和 `DisasterOperation (Drill)` 复用。该模块抽象了资源恢复和数据恢复的差异，确保各组件使用一致的恢复配置。

## 2. 核心类型定义

### 2.1 RestoreType

定义恢复类型枚举：

```go
type RestoreType string

const (
    // RestoreTypeResource 资源恢复 (从 ResourceSync 备份恢复 K8s 资源)
    RestoreTypeResource RestoreType = "resource"
    
    // RestoreTypeData 数据恢复 (从 DataSync 备份恢复 PVC 数据)
    RestoreTypeData RestoreType = "data"
)
```

### 2.2 BuilderConfig

构建配置结构体：

```go
type BuilderConfig struct {
    // RestoreType 恢复类型 (resource 或 data)
    RestoreType RestoreType

    // BackupSource AppBackup 名称或备份源标识
    BackupSource string

    // BackupName Velero Backup 名称
    BackupName string

    // TargetCluster 目标集群
    TargetCluster string

    // SourceCluster 源集群 (跨集群恢复时需要)
    SourceCluster string

    // StorageRepository 存储仓库名称
    StorageRepository string

    // IncludedNamespaces 要恢复的命名空间列表
    IncludedNamespaces []string

    // NamespaceMapping 命名空间映射 (可选，用于演练场景)
    NamespaceMapping map[string]string

    // IsForDrill 是否用于演练
    IsForDrill bool
}
```

## 3. 恢复策略矩阵

| 恢复类型 | IsForDrill | RestorePVs | ExistingResourcePolicy | ResourceModifierRules | 使用场景 |
|----------|------------|------------|------------------------|----------------------|---------|
| `resource` | `false` | `false` | `Update` | Scale-to-Zero | ResourceSync 周期同步 |
| `resource` | `true` | `false` | `Update` | Scale-to-Zero | 演练恢复资源 |
| `data` | `false` | `true` | `None` | Trafficless | DataSync 周期同步 |
| `data` | `true` | `true` | `Update` | 无 | 演练恢复数据 |

### 3.1 资源恢复 (RestoreTypeResource)

**目的**: 同步 K8s 资源定义（Deployment、StatefulSet、Service 等），不包含 Pod 和 PVC。

**Velero 配置**:
- `RestorePVs`: `false`
- `ExcludedResources`: `["pods", "persistentvolumeclaims", "persistentvolumes"]`
- `ExistingResourcePolicy`: `Update`

**ResourceModifier (Scale-to-Zero)**:
```yaml
- conditions:
    groupResource: deployments.apps
  patches:
    - operation: add
      path: /spec/replicas
      value: "0"
- conditions:
    groupResource: statefulsets.apps
  patches:
    - operation: add
      path: /spec/replicas
      value: "0"
```

### 3.2 数据恢复 (RestoreTypeData)

**目的**: 同步 PVC 数据，通过创建临时 Pod 执行文件系统级数据恢复。

**Velero 配置**:
- `RestorePVs`: `true`
- `IncludedResources`: `["pods", "persistentvolumeclaims", "persistentvolumes"]`
- `PreserveNodePorts`: `true`

**ExistingResourcePolicy**:
- 同步模式 (`IsForDrill=false`): `None` - 不覆盖已存在的资源
- 演练模式 (`IsForDrill=true`): `Update` - 覆盖已存在的资源

**ResourceModifier (Trafficless, 仅同步模式)**:
```yaml
- conditions:
    groupResource: pods
  patches:
    - operation: add
      path: /metadata/labels
      value: '{"trafficless": "true"}'
    - operation: replace
      path: /spec/containers/0/image
      value: "busybox:1.36"
```

## 4. 使用方式

### 4.1 ResourceSync Controller

```go
import restorebuilder "github.com/softcdata/testudo-operator/internal/controller/restore"

spec := restorebuilder.BuildAppRestoreSpec(restorebuilder.BuilderConfig{
    RestoreType:        restorebuilder.RestoreTypeResource,
    BackupSource:       fmt.Sprintf("rs-%s", rs.Name),
    BackupName:         backupName,
    TargetCluster:      targetCluster,
    SourceCluster:      sourceCluster,
    StorageRepository:  config.Spec.StorageRepository,
    IncludedNamespaces: instance.Spec.Namespaces,
    IsForDrill:         false,
})
```

### 4.2 DataSync Controller

```go
import restorebuilder "github.com/softcdata/testudo-operator/internal/controller/restore"

spec := restorebuilder.BuildAppRestoreSpec(restorebuilder.BuilderConfig{
    RestoreType:        restorebuilder.RestoreTypeData,
    BackupSource:       fmt.Sprintf("ds-%s", ds.Name),
    BackupName:         backupName,
    TargetCluster:      targetCluster,
    SourceCluster:      sourceCluster,
    StorageRepository:  config.Spec.StorageRepository,
    IncludedNamespaces: instance.Spec.Namespaces,
    IsForDrill:         false,
})
```

### 4.3 DisasterOperation (Drill)

```go
import "github.com/softcdata/testudo-operator/internal/controller/restore"

// 资源恢复
resourceSpec := restore.BuildAppRestoreSpec(restore.BuilderConfig{
    RestoreType:        restore.RestoreTypeResource,
    BackupSource:       resourceSync.Status.LastBackupName,
    BackupName:         resourceSync.Status.LastBackupName,
    TargetCluster:      targetCluster,
    IncludedNamespaces: instance.Spec.Namespaces,
    NamespaceMapping:   drillConfig.NamespaceMapping,
    IsForDrill:         true,
})

// 数据恢复
dataSpec := restore.BuildAppRestoreSpec(restore.BuilderConfig{
    RestoreType:        restore.RestoreTypeData,
    BackupSource:       dataSync.Status.LastBackupName,
    BackupName:         dataSync.Status.LastBackupName,
    TargetCluster:      targetCluster,
    IncludedNamespaces: instance.Spec.Namespaces,
    NamespaceMapping:   drillConfig.NamespaceMapping,
    IsForDrill:         true,
})
```

## 5. 设计原则

1. **单一职责**: 构建器只负责生成 `AppRestoreSpec`，不关心 `AppRestore` 的创建和生命周期管理
2. **配置驱动**: 通过 `BuilderConfig` 传递所有必要参数，避免隐式依赖
3. **场景区分**: 通过 `IsForDrill` 标志区分同步和演练场景，应用不同的恢复策略
4. **可扩展性**: 未来可以添加更多 `RestoreType` 或配置选项

## 6. 文件位置

```
internal/controller/restore/
└── builder.go    # AppRestore 构建器实现
```

## 7. 依赖关系

```
┌─────────────────────┐
│  DisasterOperation  │
│   (Drill Handler)   │
└─────────┬───────────┘
          │
          ▼
┌─────────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│   restore/builder   │◄────│   ResourceSync   │     │    DataSync      │
│ BuildAppRestoreSpec │     │   Controller     │     │   Controller     │
└─────────────────────┘     └──────────────────┘     └──────────────────┘
```

## 8. 变更历史

| 日期 | 版本 | 变更内容 |
|------|------|---------|
| 2026-02-09 | v1.0.0 | 初始版本，抽象 ResourceSync 和 DataSync 的恢复逻辑，支持 Drill 场景 |
