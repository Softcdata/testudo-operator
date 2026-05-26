# 容灾组并发编排与 Velero 性能调优指南

## 1. 概述
在 V2 容灾编排系统中，`DisasterGroup` 通过分层（Levels）机制实现了应用集的有序切换。该机制设计为 **Level 间串行，Level 内并行**。然而，这一逻辑层面的并行设计最终依赖于底层执行引擎 —— **Velero** 的并发处理能力。

本文档说明了 Operator 编排与 Velero 执行之间的并发关系，并提供调优建议以确保批量切换性能。

## 2. 并发模型

### 2.1 Operator 侧 (逻辑并发)
`DisasterGroup` Controller 的行为模式如下：
1.  进入一个新的 Level（例如包含 10 个实例）。
2.  在一个 Reconcile 循环内，**非阻塞地**为这 10 个实例创建对应的子 `DisasterOperation`。
3.  Kubernetes 将并发触发 10 个 `DisasterOperation` Controller 协程。
4.  这 10 个操作最终会几乎同时向 Kubernetes 提交 10 个 `AppRestore` (以及底层的 Velero `Restore` CR)。

**结果**: 在几秒钟内，Velero Server 的队列中会堆积 10 个 `Restore` 请求。

### 2.2 Velero 侧 (物理并发)
Velero Server 只要监听到 `Restore` CR 被创建就会尝试处理。但其实际并发度受限于启动参数。

*   **默认行为**: 大多数 Velero 安装默认配置较为保守，通常是 **串行** 或低并发（如并发度=1）。
*   **瓶颈现象**: 如果 Operator 并发提交了 10 个任务，而 Velero 并发度为 1，那么这 10 个任务将排队执行。`DisasterGroup` 的并行设计将退化为串行，导致整体 RTO (恢复时间目标) 显著增加。

## 3. 调优指南

为了释放 `DisasterGroup` 的并行效能，必须调整 Velero Server 的参数。

### 3.1 关键参数

| 参数 | 默认值 | 建议值 | 说明 |
|------|-------|--------|------|
| `--concurrent-backups` | 1 | **4 - 8** | 允许同时执行的备份任务数 |
| `--concurrent-restores` | 1 | **4 - 8** | 允许同时执行的恢复任务数 (关键) |

*注：建议值需根据集群资源（CPU/内存）和对象存储带宽进行实测调整。过高的并发可能导致 API Server 过载。*

### 3.2 修改方式 (Helm Chart)
如果您使用 Helm 安装 Velero，请在 `values.yaml` 的 `configuration` 部分添加参数：

```yaml
configuration:
  # 其他配置...
  extraArgs:
    # 开启并发支持
    concurrent-backups: 5
    concurrent-restores: 5
```

### 3.3 资源配额调整
提高并发度意味着 Velero Server 需要更多的 CPU 和内存来处理并行任务。请同步调整 Resource Requests/Limits。

```yaml
resources:
  requests:
    cpu: 1000m      # 增加 CPU
    memory: 1Gi     # 增加内存
  limits:
    cpu: 2000m
    memory: 2Gi
```

## 4. 最佳实践

1.  **分层控制爆炸半径**: 不要完全依赖 Velero 并发。利用 `DisasterGroup` 的 Levels 将大规模应用（如 100 个）拆分为多个批次（如 5 层，每层 20 个）。这是一种应用层的流量整形 (Traffic Shaping)。
2.  **监控 RTO**: 在演练模式 (Drill) 下观察 Group 切换的总耗时。如果发现耗时远大于单实例耗时 x 层级数，说明 Velero 侧存在排队瓶颈。
3.  **带宽预留**: 并发恢复会瞬间消耗大量网络带宽（从对象存储拉取数据），请确保集群网络带宽充足。

## 5. 常见问题
*   **Q: 为什么我的 Group 并发了，但 Pod 是一个个出来的？**
    *   A: 检查 Velero 的 `concurrent-restores` 是否为默认值 1。
*   **Q: 可以把所有应用都放在 Level 1 吗？**
    *   A: 理论上可以，但这会产生惊群效应 (Thundering Herd)，可能瞬间压垮 K8s API Server 或存储。建议合理分层。
