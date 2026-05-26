# Change: 补齐 scoped 资源过滤字段并统一恢复侧映射

## Why

当前资源过滤字段存在两类不一致：

1. `AppBackup` 走 Velero `BackupSpec`，支持 scoped 资源过滤模型（namespace-scoped / cluster-scoped）。
2. `DisasterInstance.spec.restorePolicy.resourceSelection` 仅支持旧模型（`includedResources`、`excludedResources`、`includeClusterResources`）。

这会导致前端与自动化脚本无法使用一致的字段体系表达备份与恢复范围，且在恢复侧缺少提交期冲突校验，风险会后移到执行期。

## What Changes

### 1. 扩展恢复侧字段模型

在 `DisasterInstance.spec.restorePolicy.resourceSelection` 中新增四个字段：

- `includedNamespaceScopedResources`
- `excludedNamespaceScopedResources`
- `includedClusterScopedResources`
- `excludedClusterScopedResources`

### 2. 定义恢复侧确定性映射与优先级规则

`ApplyInstanceRestorePolicy` 在处理 `resourceSelection` 时按如下顺序执行：

1. 优先级判定：
   - 当 `includeClusterResources=true` 时，恢复侧进入 old 优先路径，并忽略 scoped 四字段。
   - 其余情况按是否配置 scoped 四字段进入 scoped 映射路径。
2. old 优先路径：保持现有行为，不改变已有语义。
3. scoped 映射路径：将四个 scoped 列表确定性映射到 `velero RestoreSpec` 可表达字段：
   - `includedResources`
   - `excludedResources`
   - `includeClusterResources`

### 3. 增加提交期校验（fail-fast）

在实例创建/更新提交期执行 `resourceSelection` 校验：

- include/exclude 列表冲突校验（同一项不能同时出现）
- 通配符组合校验（例如 `exclude=["*"]` 时 include 不得有值）
- 当 `includeClusterResources=true` 时，跳过 scoped 四字段冲突校验

### 4. 明确 AppBackup 行为并补齐回归测试

`AppBackup.Spec.Template` 已直接复用 `velero BackupSpec`。本提案补齐针对 scoped 四字段的回归测试与契约说明，避免被上层误判为“不支持”。

## Non-Goals

- 不改变 `DisasterOperation` 步骤编排顺序。
- 不改动 modifierRules DSL 语义。
- 不引入新的 CRD 资源类型。

## Impact

- Affected specs:
  - `app-backup`
  - `app-restore`
- Affected code:
  - `pkg/apis/disaster/v1/disasterinstance_types.go`
  - `internal/controller/restore/policy.go`
  - `internal/controller/restore/policy_test.go`
  - `internal/webhook/disasterinstance/validator.go`
  - `internal/webhook/disasterinstance/validator_test.go`
