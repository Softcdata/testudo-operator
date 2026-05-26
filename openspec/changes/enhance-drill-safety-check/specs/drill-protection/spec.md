# Specification: DisasterDrill Safety & Target Fallback

## 1. Rationale (设计初衷)

容灾演练（Drill）的核心是执行**完整恢复（Full Restore）**。为了确保演练过程不影响生产容灾环境（Standby Environment），必须引入安全拦截机制进行强力约束。

### Risk Analysis (风险分析)
- **环境污染/覆盖**：演练恢复的资源若直接在生产备集群的生产命名空间运行，会覆盖掉为真实灾难准备的“Standby 骨架”资源。
- **脑裂风险**：乱象后的环境在发生真实切换时可能导致冲突或无法拉起。
- **数据不一致**：演练产生的数据可能与生产同步数据发生竞态冲突。

## 2. Requirements (业务逻辑)

### REQ-1: Target Cluster Fallback (目标集群自动寻址)
- **单实例演练**: 如果未指定 `targetCluster`，控制器 `MUST` 自动回退（Fallback）至实例 `status.secondaryCluster` 记录的集集群。
- **容灾组演练**: 如果未指定 `targetCluster`，演练状态回显 `status.targetCluster` `MUST` 标识为 `(Auto)`，指示演练将跟随子实例各自的备集群配置。

### REQ-2: Mandatory Safety Interceptor (强制安全拦截)
- **触发条件**: 当 `NamespaceMapping` 为空（nil 或 长度为 0）时。
- **校验规则**:
    - 控制器 `MUST` 检查选定的演练目标集群（显式指定的或自动回退的）是否与**任意**受保护实例的生产备集群（`secondaryCluster`）相同。
    - 若相同且无映射，则 **MUST** 拦截操作，将演练状态置为 `Failed`。
- **安全理由**: 在同一个集群、同一个命名空间内同时运行“容灾演练恢复”和“生产基准同步”是极度危险的操作，会导致生产备用环境损坏。

### REQ-3: Resolve & Visibility (回显与可视化)
- 所有的自动寻址和安全查核 `MUST` 在演练的 `Pending` 阶段完成。
- 控制器 `MUST` 派发原因为 `SafetyCheckFailed` 的 `Warning` 类型事件。
- 报错文案格式：`危险操作：实例 %s 的演练环境与生产备环境 (%s) 重合且未配置映射，操作将被拦截。`

## 3. Scenarios (用例场景)

| 场景 | 目标集群 | 命名空间映射 | 结果 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| **A** | 第三方测试集群 | 无 | **放行** | 目标环境隔离，安全 |
| **B** | 生产备集群 | 有 (mapping > 0) | **放行** | 逻辑命名空间隔离，安全 |
| **** | 生产备集群 | 无 | **拦截** | 会覆盖生产备环境，**危险** |
| **D** | 自动回退 (Auto) | 无 | **拦截** | 默认覆盖各实例备环境，**危险** |
