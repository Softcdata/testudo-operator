# 提案：原地改造单操作监听接口支持指定容灾组全量操作流

## 驱动因素
当前，API 提供使用 `/watch/groups/operations` 附加可选参数 `?groupName=<name>` 来监听容灾组操作。
然而，单操作监听 API (`/watch/groups/operations/:operationName`) 经常遇到使用者误传入容灾组名（`groupName`）作为操作名的情况。这导致服务端将组名当作操作名去查找 `DisasterOperation` 资源，最终导致拿不到消息并且难以排查（静默失败）。

为了简化前端调用和解决此歧义，我们决定原地改造现有的单操作监听路由 `/watch/groups/operations/:operationName`，使其能够智能探测传入的参数：如果传入的是容灾组名称，则下发该组下的所有操作事件；如果传入的是具体的单操作名称，则继续监听该唯一事件。

## 为什么 (Why)
- **开发者体验**：前端可以直接使用统一的带参数路由对容灾组操作进行追踪获取，无需繁琐拼接参数。一旦传入组名出错也会静默成功转换为组级别监听（假设组名存在）。
- **兼容性**：不新增路由保持 API 表层极简，且完全向后兼容现有的由具体操作名建立的 socket 连接，实现智能分发。
- **健壮性**：如果参数对应一个 `DisasterGroup`，则后端切换为 List+Watch 模式防止 ADDED 洪泛；若只是单个实体操作，则原样 Watch 并在连接时返回 ADDED 当前状态。

## 能力映射
- **智能组监控感知路由改造**：升级服务端 websocket 操作流处理器 `watchGroupOperation` 的底层校验逻辑与 k8s 监听配置。
- **前端容灾组操作监听 Hook 应用**：更新或规范前端 `useKubernetesWatch` 对新能力的调用（直接传入组名到操作详情 URL）。
