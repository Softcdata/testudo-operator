# 设计：V2 编排模块结构化事件发射矩阵

## 1. 设计目标

1. 将 V2 六大模块的结构化事件发射时机标准化，避免控制器各自实现。  
2. 保障运行期主备方向语义一致，尤其在 `failover -> reprotect -> 反向 sync` 链路中保持可观测一致。  
3. 以结构化 JSON 事件作为任务历史唯一事实来源，提升 Server/Web 聚合稳定性。  

## 2. 非目标

- 不改造 Event API 路由和响应模型
- 不引入新的 Event 存储后端
- 不移除所有传统 `Eventf`（仅约束其角色为诊断补充）

## 3. 事件模型

- `Reason`：`ExecutionStarted` / `ExecutionProgress` / `ExecutionFinished`
- `Type`：
  - `Normal`：开始、进行中、成功
  - `Warning`：失败、超时、取消、清理失败
- `Message`：JSON payload（至少包含 `task/status/message`）
- `Label`：`testudo.softcdata.com/task-event=true`

## 4. V2 事件发射矩阵

| 模块 | Started 时机 | Progress 时机 | Finished 时机 |
| --- | --- | --- | --- |
| DisasterInstance | 实例首次进入初始化流程 | DataSync/ResourceSync 创建成功、等待依赖就绪 | 初始化成功进入保护态；初始化失败；删除完成 |
| DataSync | 手动/调度触发一次同步 | 备份资源创建、恢复资源创建、等待 Velero 执行、临时资源清理 | 本轮同步成功；本轮同步失败 |
| ResourceSync | 手动/调度触发一次资源同步 | 资源骨架对齐、恢复任务创建、等待恢复完成、后置校验 | 本轮同步成功；本轮同步失败 |
| DisasterOperation | 操作进入执行（failover/reprotect/undo/cancel/pause/resume/sync/drill） | 每个步骤开始/推进（PreCheck/FinalSync/ScaleDown/ScaleUp/SwitchRoles 等） | 操作成功、失败、取消、超时终止 |
| DisasterGroup | 组级操作创建并开始编排 | Level 推进、实例级任务聚合进度 | 组级操作成功；组级操作失败 |
| DisasterDrill | 演练确认后进入执行；或 cleanup 启动 | 实例/组演练步骤推进、等待外部操作完成 | 演练完成；演练失败；cleanup 完成/失败 |

## 5. 运行期方向语义

### 5.1 约束

- 任何涉及 source/target 的事件描述，必须来自运行期角色判定（当前 primary/secondary），不得固定使用初始配置方向。  
- 在 `failover` 后执行 `reprotect` 与反向 `sync` 时，事件中的 `task` 与 `message` 必须体现当前方向。  

### 5.2 典型链路

1. 初始：A(primary) -> B(secondary)  
2. `failover` 后：B(primary) -> A(secondary)  
3. `reprotect` 后：以当前 primary/secondary 为准发起保护链路  
4. 后续 `sync`：继续沿运行期方向发射事件，保持与执行期一致  

## 6. 去重与收敛

- 同一资源同一步骤重复 Reconcile 时，禁止重复发同状态 Progress（需要 phase/step 防抖键）。  
- 任何终态（成功/失败/取消）必须发 `ExecutionFinished`。  
- 删除路径必须先发结束事件，再移除 Finalizer。  

## 7. 验收策略

- 单元测试：六大模块主路径+错误路径+反向路径。  
- 联调测试：Server 历史与 watch 事件连续性验证。  
- 实际演练：`failover -> reprotect -> reverse sync` 路径事件方向与执行方向一致。  
