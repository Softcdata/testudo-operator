# Design: Scoped Resource Execution Model

## 背景
当前 operator 侧没有 scoped 四字段，也没有把对象级推导结果映射到 Velero/控制器可执行表达的正式契约。

根据三轮评审结论，本 proposal 不再尝试一步到位解决“对象级精确恢复”，而是优先把能力收敛到当前链路能稳定执行的表达：kind 级资源范围。

## 关键决策

### D1. 首期选择路径 A：kind 级白名单执行
- 这是评审后的唯一稳定路径。
- 原因：
  - 当前 operator 与 Velero 构建链路天然支持 kind 级 include/exclude。
  - 当前 CRD 与 builder 不支持对象引用集合。
  - 当前也没有预打标签机制可稳定把对象级结果降维到 selector。

### D2. operator 直接补齐 scoped 四字段，与 server 已有契约对齐
- server 的 scoped API 已完成，不再额外起 companion proposal。
- operator 通过新增 scoped 四字段，承接既有 API 输出。

### D3. `includeClusterResources=true` 仍然拥有最高优先级
- 该布尔语义已存在，不在本 proposal 中重构。
- scoped 四字段只在 `includeClusterResources` 未开启时生效。

## 模型草案

### RestoreResourceSelectionPolicy
新增：
- `IncludedNamespaceScopedResources []string`
- `ExcludedNamespaceScopedResources []string`
- `IncludedClusterScopedResources []string`
- `ExcludedClusterScopedResources []string`

### 执行语义
- `IncludedNamespaceScopedResources` / `ExcludedNamespaceScopedResources`
  - 用于区分 namespace-scoped kind 的显式范围
- `IncludedClusterScopedResources` / `ExcludedClusterScopedResources`
  - 仅用于 cluster-scoped kind 范围
  - 不表达对象名、UID 或对象引用集合

## builder 行为

### DataSync / ResourceSync 备份构造
- 在生成备份范围时，允许按 scoped 四字段拆分 namespace-scoped 与 cluster-scoped kind。
- 但 cluster-scoped 侧仍只按 kind 生效。

### RestorePolicy 应用
- 恢复策略将 scoped 四字段翻译为 kind 级资源范围。
- 当遇到 `persistentvolumes`、`volumesnapshotcontents` 这类用户容易误解为对象级精确筛选的 kind 时，系统只能保证“整类资源范围”的语义。

## 明确不做的事
- 不新增对象引用集合字段。
- 不根据 namespace 动态推导具体 PV 名称列表。
- 不通过 label selector 伪装对象级精确恢复。

## 备选方案

### 方案 B：对象引用集合进入 CRD
- 放弃原因：当前没有稳定的数据载体与 builder 映射路径，体积和兼容成本过高。

### 方案 C：预打标签 / selector 降维
- 放弃原因：当前没有统一标签回填链路，且跨集群生命周期维护复杂。
