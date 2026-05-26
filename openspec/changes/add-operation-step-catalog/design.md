# Design: 容灾操作目录与步骤契约

## 背景
当前步骤语义分散在三个位置：
- `DisasterOperation` 类型定义
- `internal/controller/disasteroperation/controller.go`
- server / web 已经存在但不完整的 DTO 与页面

如果不先收敛目录，后续 proposal 很容易出现两个偏差：
- 把未来想做的步骤写成当前已经存在的步骤
- 把 message 文本误当成运行期真相源

## Goals
- 提供一个稳定的静态目录，帮助研发快速理解所有操作。
- 明确静态目录、运行期状态、事件时间线三层边界。
- 为 server 的 detail API proposal 提供可靠输入。

## Non-Goals
- 不新增 controller 字段。
- 不改动 CRD。
- 不定义 durable history 的数据模型。

## 决策

### D1. 静态目录与运行期详情必须分离
- 静态目录只负责解释语义，不承载某一次执行的真实进度。
- 某一次执行的进度必须读取 `status.currentStep`、`status.steps[]`、`status.autoCancel*`、`status.groupStatus`。

### D2. Drill 必须拆成两段理解
- `DisasterDrill.status` 负责 `Ready`、`Confirmed`、`Executing` 这类阶段。
- 底层 `DisasterOperation(type=drill)` 负责执行期步骤 `RestoreResource`、`RestoreData`、`ScaleUp`。
- 页面不能只看其中一层。

### D3. P3 的交互定义固定为“history summary + detail on demand”
- 历史列表只保留轻量信息。
- 点击某条记录以后，用该行的 `operationName` 调 server detail route。
- 运行中的操作再追加 watch。

### D4. P4 只作为依赖声明
- timeline 合流依赖 `persist-event-history-v2` 与 `add-v2-event-emission-coverage`。
- 当前变更只写清楚依赖关系，不承诺同批实现。

## 结果
- P0 文档会成为后续 server / web 设计的共同输入。
- operator 继续作为运行期步骤真相源。
