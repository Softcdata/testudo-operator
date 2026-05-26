# Change: 将命名空间相关集群资源收敛为 kind 级执行模型

## Why
当前 server 侧已经存在 scoped resource filter 的 API 契约，但 `disaster-operator` 的 `RestoreResourceSelectionPolicy` 仍只承接：
- `includedNamespaces`
- `excludedNamespaces`
- `includedResources`
- `excludedResources`
- `includeClusterResources`
- `labelSelector`

这意味着上层即使能表达 scoped resource filter，operator 也没有对应的 CRD 与 builder 执行语义。与此同时，Velero/当前控制器链路并不天然支持“按对象名精确只恢复 namespace 相关的某几个 cluster-scoped 对象”。

为了避免继续把“对象级推导”误写成可以直接落地，本 change 明确收敛首期目标：
- 先补齐 operator 对 scoped 资源选择字段的承接
- 首期只承诺 kind 级执行表达
- 不承诺对象级精确恢复

## What Changes

### 1. DisasterInstance 恢复策略补齐 scoped 四字段
在 operator 的 `RestoreResourceSelectionPolicy` 中新增并承接：
- `includedNamespaceScopedResources`
- `excludedNamespaceScopedResources`
- `includedClusterScopedResources`
- `excludedClusterScopedResources`

### 2. 首期执行表达固定为 kind 级资源范围
- `includedClusterScopedResources` / `excludedClusterScopedResources` 仅表达“资源种类”，不表达具体对象名。
- builder 只把这些值翻译成 kind 级选择范围，不把它们解释成对象级精确集合。

### 3. includeClusterResources 继续保持最高优先级
- 当 `includeClusterResources=true` 时，operator 继续按“包含所有 cluster-scoped 资源”路径处理。
- scoped 四字段在该路径下不生效。

### 4. 对象级精确恢复显式作为后续能力
- 首期不支持“只包含与 namespace 关联的某几个 PV / VolumeSnapshotContent 对象”。
- 若后续确需对象级精确恢复，应另起 proposal 处理对象集合承载与执行表达。

## Non-Goals
- 不修改 server 已完成的 scoped resource filter API 契约。
- 不在首期定义对象引用集合字段。
- 不承诺 PV、VolumeSnapshotContent 的对象级精确恢复。

## Impact
- Affected specs:
  - `disaster-instance`
- Affected code:
  - `pkg/apis/disaster/v1/disasterinstance_types.go`
  - `internal/controller/restore/policy.go`
  - `internal/controller/datasync/controller.go`
  - `internal/controller/resourcesync/controller.go`
- Cross-repo impact:
  - `disaster-server`：使用既有 scoped API 契约，无需同步新 proposal
  - `cluster-disaster-web`：若提供“自动寻找”能力，首期只能展示 kind 级结果

## Relationship to Existing Changes
- server 已有已完成 change：`add-scoped-resource-filter-api-support`
- 本 change 不重复新增 API 字段，只补 operator 执行模型与 CRD 承接。

## Risks
- 用户可能误以为 `includedClusterScopedResources` 代表对象级精确集合，需要在 API 文档和 UI 提示中明确纠偏。
- 若首期支持的 kind 范围定义过大，仍可能引入过量恢复。
