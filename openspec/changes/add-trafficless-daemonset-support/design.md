# Design: 长明火 DaemonSet 的目标端 Placement

## Context

ResourceSync 的资源备份会保留 DaemonSet，但当前 shared restore builder 只对 deployments.apps 和 statefulsets.apps 写入 /spec/replicas=0。DaemonSet 没有该字段，所以不能通过复制现有规则获得正确行为。运行期的副本记录、ScaleDown、ScaleUp、CheckReplicas 和 Drill cleanup 也只枚举前两类工作负载。

前一版设计把源 DaemonSet 的 nodeSelector 写入 ConfigMap，计划在目标激活时还原。该方案不成立：源端与目标端节点的名称、标签、可用区、污点和 nodeAffinity 往往不一致。源端选择器不是目标端调度策略，控制器也无法可靠推断两套标签之间的业务等价关系。

仓库现有 RestorePolicy modifierRules 已具备所需基础：reversible pair 支持 JSON object、array、null 和 string 值，并按当前主备方向选择 sourceValue 或 targetValue。因此本变更复用该 contract，不新建 DaemonSet 拓扑 API。

## Goals

- DaemonSet 待机、激活、回退和 Drill 使用显式的目标端 placement，而非复制源端 placement。
- 没有明确的跨集群 mapping 时失败关闭，避免 Pending 或错误运行到全部节点。
- 保持源/目标角色反转后的双向可逆语义。
- 让待机 restore 始终无业务流量，激活才写入冻结后的目标 placement。
- 保持 Deployment/StatefulSet、DataSync 数据恢复和既有 RestorePolicy contract 不变。

## Non-Goals

- 不依据节点数量、名称、标签前缀或 zone 名称自动生成映射。
- 不把所有 DaemonSet 的 target nodeSelector 默认为空。
- 不要求完整模拟 Kubernetes Scheduler；预检验证 placement 的静态候选节点，运行期继续以 DaemonSet status 收敛为准。
- 不扩展用户 DSL 到脚本、模板或另一个 DaemonSet 专用字段。

## Decision 1: 待机隔离仍使用系统保留 selector

每个 DisasterInstance 生成稳定的保留待机 selector：

- key: testudo.softcdata.com/trafficless-standby
- value: 由实例 UID 派生的稳定短标识

ResourceSync restore 为 daemonsets.apps 生成 system-protect ResourceModifierRule，将 /spec/template/spec/nodeSelector 覆盖为仅含该 selector 的 map。该规则先列出目标集群节点，只有确认没有节点匹配才允许提交 restore。

系统 selector 只表达“不得运行”，不表达业务目标拓扑。它在待机 restore 时必须优先于用户 target nodeSelector；否则用户规则会解除隔离。它不会替换 target placement 快照。

## Decision 2: 用可逆 modifier rules 声明目标端 placement

每个被保护的 DaemonSet 必须存在一条唯一、可逆、按当前方向生效的 nodeSelector pair，适用于 resourceSync 和 drill。规则以 daemonsets.apps、namespace 和 resourceNameRegex 精确选择 workload。

逻辑示例：

    mode: reversible
    applyTo: [resourceSync, drill]
    conditions:
      groupResource: daemonsets.apps
      namespaces: [app]
      resourceNameRegex: ^node-agent$
    pair:
      path: /spec/template/spec/nodeSelector
      sourceValue: {"node-role.kubernetes.io/edge":"true"}
      targetValue: {"dr.testudo.io/node-pool":"agent"}

角色反转后，同一 pair 自动选择 sourceValue，因此无需维护第二份反向规则。

pair 的 sourceValue 和 targetValue 分别绑定 DisasterConfig 的 baseline source/target，而不是任意运行时集群。它只能安全表达这一对集群的 placement。本变更不把 baseline targetValue 解释为任意 Drill target 的通用值：Drill 的 targetCluster 必须等于当前方向解析后的目标集群和 placement 快照的 targetCluster；否则只要保护范围包含 DaemonSet，Drill 必须在恢复或激活前失败关闭。第三集群的 DaemonSet 映射需要新增多目标 contract，不能由二元 pair 猜测。

如果源 DaemonSet 的以下字段非空，也必须有同一 workload 范围的可逆 pair 指定目标值或显式清除值：

1. /spec/template/spec/affinity
2. /spec/template/spec/tolerations
3. /spec/template/spec/topologySpreadConstraints
4. /spec/template/spec/schedulerName

对这些字段，targetValue 可以是目标端 JSON object、array、string 或 null。sourceValue 必须与同步时源对象的真实字段值一致；不一致说明配置或工作负载已漂移，必须中止本轮同步并要求更新 mapping。

不允许依赖 veleroNative 的单向 patch 作为 DaemonSet placement：它不能保证 Failover 后的反向映射，也不能作为激活快照的规范来源。

## Decision 3: 保存解析后的目标 placement，而非源端 selector

现有 replicas-<resourceSyncName> ConfigMap 保留 replicas 键兼容 Deployment/StatefulSet。新增独立版本化键 daemonset-placement-v1，按源 namespace 和 DaemonSet 名称存储：

    {
      "app/daemonsets/node-agent": {
        "targetCluster": "cluster-dr",
        "targetPlacement": {
          "nodeSelector": {"dr.testudo.io/node-pool":"agent"},
          "affinity": null,
          "tolerations": [],
          "topologySpreadConstraints": [],
          "schedulerName": null
        },
        "standbySelector": {
          "testudo.softcdata.com/trafficless-standby":"instance-abc123"
        },
        "ruleFingerprint": "sha256:..."
      }
    }

nil 与空值必须可区分。快照的 source placement 仅保留可选审计摘要或 hash，不能作为激活输入。

写入步骤：

1. ResourceSync 从当前 source 集群读取 DaemonSet，解析与验证所有 placement pair。
2. 控制器按当前 source/target 角色选择目标值，验证 target nodeSelector 和 required nodeAffinity 至少有一个目标 Node 候选。
3. 验证通过后写入 daemonset-placement-v1，再构建待机 AppRestore。
4. AppRestore 使用 system-protect standby selector；快照保留激活时要写入的 target placement。

对映射 namespace Drill，快照 key 仍使用源 namespace，实际对象定位使用映射后的 namespace。namespaceMapping 只改变对象命名空间，不改变 pair 的目标集群语义；Drill 目标必须与快照 targetCluster 一致。

## Decision 4: 统一运行期的 DaemonSet 生命周期

实现阶段应抽取共享的待机/激活辅助函数，避免 executeScaleDownSource、doScaleDown、executeScaleUpTarget、doScaleUp 和 Drill cleanup 分叉。

| 阶段 | Deployment/StatefulSet | DaemonSet |
| --- | --- | --- |
| ResourceSync 待机恢复 | replicas=0 | system-protect 待机 selector |
| Failover ScaleDownSource | replicas=0 | 写入待机 selector |
| Failover ScaleUpTarget | 恢复副本数 | 从目标 placement 快照写入目标调度字段 |
| Undo/Cancel/Reprotect | 调用公共缩放函数 | 同一公共待机/激活函数，按当前方向读取目标快照 |
| Drill ScaleUp | 恢复副本数 | 映射 namespace 中写入 Drill 解析的目标 placement |
| 无映射 Drill cleanup | replicas=0 | 重新写入待机 selector |
| 有映射 Drill cleanup | 删除映射 namespace | 删除映射 namespace |

激活只能处理带当前实例待机 selector 的 DaemonSet。标记存在但快照缺失、targetCluster 不匹配或 ruleFingerprint 漂移时失败；标记不存在时不得覆盖用户后来修改的字段。

## Decision 5: 校验目标 placement 和 DaemonSet 收敛

在 ResourceSync 预检和 Operation 激活前：

1. sourceValue 必须与源 DaemonSet 当前字段一致。
2. target nodeSelector 与 required nodeAffinity 必须能匹配至少一个目标 Node，且该 Node 不得因未被目标 tolerations 容忍的 NoSchedule 或 NoExecute taint 被静态排除。
3. 资源压力、调度器实现、Pod affinity/anti-affinity 和其他运行时限制仍无法仅靠静态检查证明可调度；WaitUntilReady 必须继续观察 DaemonSet status 并返回具体阻塞信息。

CheckReplicas 和 waitUntilReady 增加 DaemonSet 检查：

1. status.observedGeneration 必须追上 metadata.generation。
2. status.numberUnavailable 必须为 0。
3. status.numberAvailable 必须等于 status.desiredNumberScheduled。
4. 尚带待机 selector、目标候选节点为零或 placement 快照无效的 DaemonSet 必须失败，不能将 desiredNumberScheduled=0 视为成功。

## Decision 6: DataSync 保持 Pod 级 Trafficless 语义

DataSync 不把 DaemonSet 加入 IncludedResources。DaemonSet 所属 Pod 已经命中 pods ResourceModifierRule，实施时补充测试证明其会：

1. 只保留 trafficless 标签。
2. 清空 ownerReferences。
3. 清理 nodeName、nodeSelector 和 affinity。
4. 使用当前目标集群解析的 Trafficless 镜像、command、args 和 imagePullSecrets。
5. 在 DataSync 成功或 Drill cleanup 时按 trafficless 标记被清理。

这避免临时 Pod 被 DaemonSet Controller 接管，也避免把业务 DaemonSet 模板误用于数据恢复。

## Image Mapping Dependency

DaemonSet 的 target placement 解决调度位置，不解决业务镜像可拉取性。实现前必须确认当前动态镜像改写链路能够扫描 daemonsets.apps 的 containers 和 initContainers；若当前实际链路尚未覆盖，相关扩展必须与本变更一同合入，并在未覆盖时失败关闭。

## Risks and Mitigations

| Risk | Mitigation |
| --- | --- |
| 源/目标节点标签不同 | 不复制源 selector；每个 DaemonSet 用可逆 pair 显式声明 target placement。 |
| 目标 placement 未配置 | 同步前失败关闭，不默认清空 selector。 |
| 源工作负载或 mapping 漂移 | 对 sourceValue 做结构化比对，规则不匹配即拒绝快照。 |
| 待机 restore 被用户规则解除 | standby selector 作为 system-protect 规则，优先于 target nodeSelector 规则。 |
| target label、affinity 或 taint 仍使 Pod Pending | 预检候选 Node，WaitUntilReady 继续观察 status 并输出阻塞原因。 |
| Drill 指向第三集群 | pair 仅覆盖配置基线两端；快照 targetCluster 不匹配时，在恢复或激活前失败关闭。 |
| 用户手工修改目标 DaemonSet | 仅处理带当前待机 selector 的对象；无标记对象不覆盖。 |
| 旧 ConfigMap 无 placement | 标记对象不可激活；下一次成功 ResourceSync 生成目标 placement 后才允许操作。 |

## Migration and Rollback

1. 先为每个受保护 DaemonSet 配置并提交 target placement reversible pairs。
2. 部署带 placement 校验和 ConfigMap 兼容读取的 Operator。
3. 触发一次 ResourceSync，验证 target placement 快照与待机 selector 均成功写入。
4. 再允许包含 DaemonSet 的 Drill 和 Failover。
5. 回滚前先对仍带待机 marker 的 DaemonSet 显式应用其 target placement 快照，或人工选择目标 placement；不得回滚后遗留无法激活的 DaemonSet。

没有新 CRD schema。已有 RestorePolicy modifier rules 是唯一用户输入，ConfigMap 新数据键可由旧 Operator 忽略。

## Open Questions

- 需要为每个生产目标集群梳理 DaemonSet 的等价 node labels、required nodeAffinity 和 taint/toleration 规则，再编写 pair 值。
- 如果业务要求把包含 DaemonSet 的 Drill 运行到第三集群，需要单独设计显式的多目标 placement contract，而不是扩展本 pair 的隐式解释。
- 需要确认动态镜像改写的当前提交是否已经覆盖 DaemonSet；若未覆盖，应在合并队列中与本变更绑定。
