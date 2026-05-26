# 执行规划：V2 结构化事件发射覆盖

## 1. 目标与边界

### 1.1 目标
- 让 V2 六大编排模块的任务事件满足统一标准：`ExecutionStarted -> ExecutionProgress -> ExecutionFinished`。
- 确保 failover 后的 reprotect / reverse sync 事件方向与运行期主备角色一致。
- 保证 Server 与 Web 在现有接口下可稳定聚合与展示 V2 任务时间线。

### 1.2 边界
- 本变更只处理 V2 编排链路：`DisasterInstance`、`DataSync`、`ResourceSync`、`DisasterOperation`、`DisasterGroup`、`DisasterDrill`。
- 不引入新的 API 路由和返回字段。
- 不重构与本提案无关的控制器逻辑，不删除无关代码。

## 2. 实施顺序（分阶段）

## 阶段 A：测试用例先行（必须先完成）
1. 为六大模块各自建立事件断言测试：
   - Started/Finished 成对出现
   - 关键步骤存在 Progress
   - 失败路径最终收敛到 Finished(Warning)
2. 单独增加“方向一致性”场景：
   - `failover -> reprotect -> syncresource`
   - 断言事件中的 source/target 描述随运行期角色变化
3. 增加“删除路径”场景：
   - 断言先发 Finished，再移除 Finalizer

输出物：
- 模块级 `_test.go` 增量
- 覆盖率与失败用例记录

## 阶段 B：控制器改造（按模块由小到大）
1. `DisasterInstance`：先补初始化与删除路径事件。
2. `DataSync`、`ResourceSync`：补齐触发/步骤/终态事件（含 schedule/manual）。
3. `DisasterOperation`：补齐 failover/reprotect/undo/pause/resume/sync/drill 事件矩阵。
4. `DisasterGroup`、`DisasterDrill`：补齐分层推进和清理路径事件。

统一改造规则：
- 使用 `pkg/helper/event_reporter.go` 的 `ReportTask*WithClient`。
- 诊断类 `Recorder.Eventf` 允许保留，但不能替代结构化任务事件。
- 对重复 Reconcile 增加 phase/step 防抖，避免重复 Progress 噪声。

输出物：
- 控制器事件发射代码改动
- 与 `global-events` 规范一一对照的实现说明

## 阶段 C：联调与验收
1. Operator 侧验收：结构化事件标签、载荷字段、生命周期完整性。
2. Server 侧验收：`/apis/v1/events` 历史聚合与 `/apis/v1/watch/events` 实时流。
3. Web 侧验收：事件通知与任务历史完整展示。
4. 真实环境回归：基于 `c170/c171` 跑 failover + reprotect + reverse sync。

输出物：
- E2E 结果记录（主路径/错误路径/反向路径）
- 回归结论与风险清单

## 3. 模块落地检查清单

### 3.1 DisasterInstance
- 创建实例时发 Started + Finished
- 初始化过程关键节点发 Progress
- 删除流程在 Finalizer 移除前发 Finished

### 3.2 DataSync
- schedule/manual 触发均发 Started
- 备份创建、恢复创建、等待执行发 Progress
- Success/Failed 发 Finished

### 3.3 ResourceSync
- schedule/manual 触发均发 Started
- 资源骨架/恢复/后置校验发 Progress
- Success/Failed 发 Finished

### 3.4 DisasterOperation
- 每种 operationType 发 Started
- 步骤切换发 Progress（PreCheck/FinalSync/ScaleDownSource/ScaleUpTarget/SwitchRoles 等）
- Completed/Failed/Canceled/TimedOut 发 Finished
- failover 后 reprotect/sync 方向文案按运行期角色

### 3.5 DisasterGroup
- 组级操作开始发 Started
- Level 推进发 Progress
- 全组终态发 Finished

### 3.6 DisasterDrill
- Ready->Executing 发 Started
- 执行步骤发 Progress
- Completed/Failed/CleanedUp 发 Finished

## 4. 风险控制

1. 事件风暴风险：通过 phase/step 去重控制同状态重复发射。
2. 方向语义漂移：统一调用运行期角色解析函数，禁止使用静态方向字段拼接文案。
3. 删除路径事件丢失：强制遵循“先 Finished 后移除 Finalizer”顺序。

## 5. 验收门禁

1. `openspec validate add-v2-event-emission-coverage --strict` 通过。
2. 六大模块事件矩阵均有测试覆盖（主路径+错误路径）。
3. 反向路径（reprotect/reverse sync）方向语义通过 E2E 验证。
4. 不包含与本提案无关的代码变更。
