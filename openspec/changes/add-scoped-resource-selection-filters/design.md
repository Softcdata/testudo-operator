## Context

V2 场景需要统一表达资源过滤范围。

- 备份侧使用 `BackupSpec`，支持 scoped 四字段。
- 恢复侧 `RestoreSpec` 仅支持旧模型字段。
- `DisasterInstance.restorePolicy.resourceSelection` 当前仅暴露旧模型，无法与备份侧字段体系对齐。

## Goals / Non-Goals

- Goals:
  - 在实例恢复策略中补齐 scoped 四字段。
  - 给出确定性优先级与映射规则，避免运行时歧义。
  - 在提交期拒绝真正非法配置，防止风险后移。
- Non-Goals:
  - 不改动 Velero 原生 `RestoreSpec` 结构。
  - 不改动 Failover / Reprotect 编排步骤。

## Decisions

### Decision 1: includeClusterResources=true 作为恢复侧优先级开关

`resourceSelection` 支持 old 字段与 scoped 字段并存，但采用确定性优先级：

- 当 `includeClusterResources=true` 时，优先采用 old 路径，忽略 scoped 四字段。
- 当 `includeClusterResources` 非 true 时，若配置了 scoped 四字段，则走 scoped 映射路径。

理由：

- 满足上层“显式选择 includeClusterResources=true 时不读取 scoped 字段”的兼容诉求。
- 避免调用方因字段并存被直接拒绝。

### Decision 2: scoped 模式映射到 RestoreSpec

在 scoped 映射路径下：

1. `Template.IncludedResources` = `includedNamespaceScopedResources + includedClusterScopedResources` 去重后顺序合并
2. `Template.ExcludedResources` = `excludedNamespaceScopedResources + excludedClusterScopedResources` 去重后顺序合并
3. `Template.IncludeClusterResources` 由 cluster-scoped 字段确定：
   - `excludedClusterScopedResources == ["*"]` -> `false`
   - 其余 cluster-scoped 条件存在 -> `true`
   - 无 cluster-scoped 条件 -> 保持 `nil`

理由：

- 不改动 Velero 类型即可表达 scoped 输入。
- 对 cluster 资源的处理行为可预测。

### Decision 3: 提交期 fail-fast 校验

在 webhook 阶段完成以下校验并拒绝不合法请求：

1. include/exclude 交集冲突
2. `*` 组合冲突
3. 当 `includeClusterResources=true` 时，跳过 scoped 四字段冲突校验

理由：

- 防止高风险策略进入执行阶段。
- 保留优先级兼容行为，避免过度拦截。

## Risks / Trade-offs

- 风险：字段并存时，调用方可能不清楚哪些字段被忽略。
  - 缓解：在错误/事件/注解中输出生效模式（old/scoped）便于排障。
- 风险：新增优先级会让“同一请求不同字段”的行为依赖布尔开关。
  - 缓解：文档明确 `includeClusterResources=true` 的覆盖语义。

## Migration Plan

1. 扩展 API 类型并生成 CRD。
2. 实现优先级判定、映射与校验函数。
3. 接入 webhook。
4. 增加单元测试覆盖 old 优先、scoped 映射、冲突路径。
5. 更新 server 与接口文档契约。

## Open Questions

- 是否需要在实例详情接口回显 `resourceSelectionResolvedMode`（old/scoped）。
