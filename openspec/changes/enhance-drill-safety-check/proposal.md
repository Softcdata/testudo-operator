# OpenSpec: DisasterDrill 目标集群自动回退与安全检测增强

## Why (背景)

当前容灾演练（DisasterDrill）在处理目标集群（TargetCluster）时存在以下不足：
1. **回显不统一**：实例演练在不传目标集群时能自动回显备集群名称，但容灾组演练目前在 Status 中留空，用户无法在确认前直观看到执行位置。
2. **安全检测缺失**：容灾组演练在未配置 `NamespaceMapping` 时，如果目标集群恰好是成员实例的生产备集群，应当像单实例演练一样触发拦截保护，防止覆盖生产容灾环境。

## What Changes (核心设计)

### 1. 目标集群自动回退逻辑对齐
- **实例演练**：保持现状，若 `Spec.TargetCluster` 为空，自动回退到 `Instance.Status.SecondaryCluster` 并写入 `Status.TargetCluster`。
- **容灾组演练**：若 `Spec.TargetCluster` 为空，在 `Status.TargetCluster` 中写入 `"(Auto)"` 或标记位，且下游透传给 `DisasterOperation` 时保持为空，由子操作执行时动态寻址。

### 2. 容灾组安全检测增强 (Protection)
- **拦截逻辑扩展**：在 `DisasterDrill` 的 `handlePending` 校验阶段，如果是容灾组演练：
    - 若 `Spec.NamespaceMapping` 为空：
        - 遍历该容灾组内的**所有实例**。
        - 检查演练目标集群（统一指定的或回退后的）是否与任意一个实例的 `SecondaryCluster` 相同。
        - 若存在相同，则拦截并置为 `Failed`，报错：“危险操作：容灾组内实例 %s 的演练环境与生产备环境重合且未配置映射，操作已被拦截”。

### 3. 文案润色与统一
- 统一拦截事件原因为 `SafetyCheckFailed`。
- 报错文案支持中英文对齐。

## Implementation (实施计划)

### disaster-operator
1. **修改 `DisasterDrillReconciler.validateGroupDrill`**:
    - 增加对容灾组内所有成员实例的加载和拓扑检查。
    - 实现跨实例的 `SecondaryCluster` 冲突校验逻辑。
2. **修改 `DisasterDrill` 状态更新**: 确保 `Pending` 阶段结束后，无论哪种模式，`Status.TargetCluster` 都有明确的可读回显。

### disaster-server (可选)
1. 在 `CreateDrill` 接口文档或相关提示中明确此保护机制。
