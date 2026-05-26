# Proposal: 修复容灾组中失效的实例引用 (Fix Broken References in Disaster Group)

## Why
目前，用户可能会在一个 `DisasterInstance` 被加入 `DisasterGroup` 的情况下，直接将其删除。这将导致 `DisasterGroup.Spec.Levels` 中产生无效的僵尸引用。
一旦尝试在该组上执行容灾演练、切换或停止等操作，`DisasterOperation` 调谐器就会因为查询不到该实例而直接报错 `DisasterInstance 未找到`，从而导致整个组的操作直接失败挂起。

更糟糕的是，目前在 `disaster-server` 暴露的接口中（`GET /groups/:name/instances`），由于直接跳过了（`continue`）未找到的实例，这使得前端页面无法正确渲染这个“已失效的成员”，用户无法在界面上选中该实例进行移除，导致整个灾备组陷入无法使用的死锁状态。

## What Changes
1. **Operator (删除保护)**：在 `DisasterInstanceReconciler.handleDeletion` 中引入组引用检查。在删除实例前，如果查询到该实例被配置在某一个现存的 `DisasterGroup.Spec.Levels` 中，则直接**阻止实例删除**（发送 DeletionBlocked Event）。除非用户显式加入 `testudo.softcdata.com/force-delete=true` 标签，强制走完清理流程。
2. **Server API 的透明化展示**：修改 `disaster-server` 的 `GroupHandler.listGroupInstances` 处理逻辑。在查询某个特定层级下的实例列表时，如果因为实例已被删除导致 K8s API 报 `NotFound`，不再静默忽略（`continue`），而是向列表返回一个 `FsmState: "NotFound"` 的占位 DTO。同时返回该实例的 Name。
这样，用户能够在前端界面上看到这个异常（丢失）的实例，并可以通过正常的“编辑组”流程将这行名字移出组。
3. **编辑容灾组能力完善 (Server 端)**：为了能让前端配合修复，提案明确纳入 `PUT/PATCH /groups/:name` 接口的作用，规范该接口**必须同时支持修改 `Levels`（调整实例所在的容灾编排层级，或者添加/删除容灾实例）以及说明标签（`Description`）**。这确保当您发现组内存在幽灵实例或需要变动架构时，能够直接保存新的实例架构完成自愈。
4. **Operator (Operation 健壮性增强)**：在 `DisasterOperation` 中执行 Group Operation 遍历所有底层实例时，如果配置了 `FailPolicy: Continue`（继续策略），那么即使查不到下属的实例，也不应该直接中断，可以通过警告事件绕过该失效节点。

## Alternatives Considered
- 我们曾考虑是否让 `DisasterInstance` 控制器在删除前直接去寻找并改写所有相关 `DisasterGroup.Spec.Levels`（级联清理）。这种方案被否决，因为跨控制器的 Spec 状态修改不仅非常容易带来竞态条件（Race Conditions），且违背了 K8s “由声明来控制”的原则。类似 PVC 与 Pod 之间的强绑定关系，直接阻止被引用的对象删除是业界最佳实践。
