# Implement Disaster Recovery Logic V2 (V2 容灾逻辑实现)

## Summary
本提案旨在基于已验证的 V2 容灾编排框架 (`implement-disaster-orchestration-v2`)，填充具体的数据同步、资源同步和故障切换业务逻辑。

## Motivation
V2 编排框架已经实现了状态机流转、CRD 联动、Cron 调度和集成测试，但实际的同步操作和故障切换步骤仍为 Stub（桩代码）。其中最关键的缺失是**跨集群操作能力**——目前的桩代码仅在 Operator 所在集群执行操作，无法满足源集群和目标集群分离的真实容灾场景。

## Goals
1.  **多集群 Client 管理**: 实现从 `Cluster` CR 动态构建 `client.Client` 的机制，使 Operator 能操作远程集群。
2.  **Failover 完整逻辑**: 实现 Pause -> ScaleDown (Source) -> FinalSync -> ScaleUp (Target) 的完整流程。
3.  **Failback 完整逻辑**: 实现反向切换流程。
4.  **DataSync/ResourceSync 增强**: 完善 Trafficless Restore 和 Replicas=0 的具体实现（依赖于多集群 Client）。

## Technical Design

### 1. 多集群 Client 管理 (`ClusterClientFactory`)
Operator 需要根据 `DisasterInstance` -> `DisasterConfig` -> `Cluster` 的引用链，动态获取源集群和目标集群的访问权限。

- **组件**: `pkg/clusterclient/factory.go`
- **功能**:
    - 接收 `Cluster` CR 名称和 Namespace。
    - 读取 `Cluster` CR 中的 KubeConfig (Secret 引用)。
    - 构建并缓存 `controller-runtime` 的 `client.Client`。
    - 提供 `GetSourceClient` 和 `GetTargetClient` 辅助方法。

### 2. Failover 逻辑详解

Failover 操作由 `DisasterOperation` Controller 协调，包含以下串行步骤：

#### Step 1: PauseSchedules
- **目标**: 停止所有自动同步，防止在切换过程中产生新的数据。
- **实现**:
    - 获取关联的 `DataSync` 和 `ResourceSync` 资源。
    - Patch `spec.Paused = true`。
    - 更新 `DisasterInstance` 状态为 `FailingOver`。

#### Step 2: ScaleDownSource
- **目标**: 停止源集群的应用，确保数据一致性（停止写入）。
- **实现**:
    - 使用 `ClusterClientFactory` 获取 **Source Cluster Client**。
    - 遍历 `DisasterInstance` 保护的 Namespaces。
    - List所有 Deployment/StatefulSet。
    - 记录当前 Replicas 到 Annotation `testudo.softcdata.com/original-replicas`。
    - 更新 Replicas 为 0。

#### Step 3: FinalSync
- **目标**: 同步 ScaleDown 期间或之后产生的最后一份数据/状态。
- **实现**:
    - 触发 `DataSync` 和 `ResourceSync` 的 `spec.trigger.manual`。
    - 轮询等待两者的 Status 变为 Ready 且 `LastSyncTime` 更新。
    - 此步骤确保目标集群的数据是最新的。

#### Step 4: ScaleUpTarget
- **目标**: 在目标集群拉起应用。
- **实现**:
    - 使用 `ClusterClientFactory` 获取 **Target Cluster Client**。
    - **清除 Trafficless 标记**: 移除 Service 上的 `testudo.softcdata.com/trafficless` Selector（如果存在），允许流量进入。
    - **恢复 Replicas**:
        - List 所有 Deployment/StatefulSet (此时它们应该是 0 副本)。
        - 读取 Annotation `testudo.softcdata.com/original-replicas`。
        - 如果无 Annotation，默认设为 1。
        - 更新 Replicas。

#### Step 5: SwitchRoles
- **目标**: 更新元数据，正式通过切换。
- **实现**:
    - 交换 `DisasterInstance` 的 `PrimaryCluster` 和 `SecondaryCluster`。
    - 更新状态为 `Active`。

### 3. Failback 逻辑
基本流程与 Failover 镜像，但方向相反。需要注意的是，Failback 通常意味着数据已经反向同步完毕（这部分由 DataSync 反向同步负责，暂不在本 Scope，假设数据已就绪）。本阶段主要关注应用层的反向切换。

## Timeline
- **Start Date**: 2026-01-07
- **Target Date**: 2026-01-14
