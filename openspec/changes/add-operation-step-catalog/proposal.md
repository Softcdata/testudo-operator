# Change: 新增容灾操作目录与步骤契约

## Why
当前项目里“容灾操作步骤”并不缺：
- operator 已把 `failover`、`undo`、`cancel`、`drill` 等运行期步骤写进 `DisasterOperation.status`
- `DisasterDrill` 也已经回显 `currentStep` 与 `steps[]`

真正缺的是统一入口：
- 开发不了解每种操作当前真实有哪些步骤
- server proposal 很容易把“产品预期步骤”写成“代码当前已实现步骤”
- web 无法区分“静态目录”和“某一次操作的运行期详情”

因此首期必须先把操作目录与步骤契约文档化，再让后续 API / UI 变更围绕这份目录落地。

## What Changes

### 1. 新增 P0 静态操作目录
- 新增 `docs/harness/operation-visibility-catalog.md`
- 目录必须逐项回答：
  - 操作类型属于 `Instance`、`Group` 还是 `Drill`
  - 代码里当前稳定存在哪些步骤
  - 运行期应读取哪些状态字段
  - 当前 server 哪些接口已暴露，哪些仍缺失

### 2. 明确步骤真相源分层
- 静态目录只负责解释“有哪些步骤、字段在哪里”。
- 运行期详情只认 `DisasterOperation.status` 与 `DisasterDrill.status`。
- 事件时间线属于后续 P4，不在 P0 中替代运行期状态。

### 3. 把 P0-P4 的边界写成正式提案输入
- P0：静态目录
- P1：drill 现有 DTO 的页面展示
- P2：server 补 detail API
- P3：历史列表点击记录后再调 detail API
- P4：durable history / timeline 合流

## Non-Goals
- 不修改 controller 逻辑。
- 不新增新的步骤枚举。
- 不替代 server 的 detail API proposal。
- 不在本变更里实现 web 页面。

## Impact
- Affected specs:
  - `operation-catalog`
- Affected docs:
  - `docs/harness/operation-visibility-catalog.md`
- Cross-repo impact:
  - `disaster-server`：以目录作为 detail DTO 与 history 标识设计输入
  - `cluster-disaster-web`：以目录作为页面信息架构输入
