# 任务清单 (Tasks)

## 1. 容灾操作切换角色逻辑的抗冲突重构
- [ ] 针对负责 `DisasterOperation` 中处理 `SwitchRoles` (切换主备集群) 的功能模块，编写并使用可靠的 `retry.RetryOnConflict` 包裹层。
- [ ] 在重试循环体内，务必使用 `client.Get` 动态地从最上游 Apiserver 重新拉取一次当前关联的 `DisasterInstance`，以绝不使用之前陈旧或被缓存的副本去执行赋值 `latest.Status.PrimaryCluster = targetCluster`。

## 2. 其余操作状态变化的同类代码审查
- [ ] 对于诸如 `Undo` (回滚角色撤回)、`PreCheck` 以及任何影响 `DisasterInstance.Status` 内容流向的步骤，进行同类 `RetryOnConflict` 审计并增加必要保护，对抗突发更新竞争。

## 3. (可选但推荐) 验证日程表级别的竞态规避控制
- [ ] 审计 `PauseSchedules` 或者对应的 `DataSync` 流程代码。确认哪怕当背景进程被挂起时，如果有残存正执行写回操作的协程，无论其什么情况写回，依然不影响上面第 1 条的绝对覆盖更新原则。
