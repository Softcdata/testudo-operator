# 提案：Failover 支持跳过源集群缩零 (skipScaleDownSource)

## 背景
当前 `DisasterOperation` 的 Failover 流程支持通过 `skipFinalSync` 跳过最终同步，但不支持“仅跳过源集群缩零（ScaleDownSource）”。

在部分应急演练或受限场景中，用户希望：
- 保留源集群业务副本不缩零；
- 同时仍然触发目标集群拉起与角色切换；
- 并且该行为由显式参数控制，而不是通过修改默认流程实现隐式分支。

目前 Operator 在 `ScaleDownSource` 步骤无条件执行缩零，导致无法满足该诉求。

## 目标
1. 为 Failover 新增可选参数 `spec.skipScaleDownSource`（默认 `false`）。
2. 当参数为 `true` 时，Failover 在 `ScaleDownSource` 步骤显式跳过缩零，继续后续步骤。
3. 在实例级与组级操作中都支持该参数透传。
4. 保持默认行为兼容，未设置参数时流程不变。

## 非目标
1. 不改变 Failover 的默认步骤顺序。
2. 不默认开启任何“自动跳过缩零”策略。
3. 不在本提案中引入自动脑裂检测/自动回滚。

## 方案概述

### 1) CRD 字段扩展
在 `DisasterOperationSpec` 新增：
- `skipScaleDownSource: bool`（optional）

语义：
- 仅对 `operationType=failover` 生效；
- `true` 表示跳过 `ScaleDownSource` 步骤；
- `false` 或未传表示保持当前行为。

### 2) Failover 执行语义
- 在执行 `ScaleDownSource` 步骤前判断该参数；
- 若为 `true`，则不执行源集群副本缩零逻辑，直接视为步骤完成并继续后续步骤；
- 需要记录可审计信号（Event + 状态消息）说明该步骤被显式跳过。

### 3) Group 子操作透传
`DisasterGroup` 触发的子 `DisasterOperation` 在创建时需透传 `skipScaleDownSource`，保证组操作和单实例操作语义一致。

### 4) 跨项目联动（server）
`disaster-server` 的操作入口需要支持并透传 `skipScaleDownSource`：
- 实例操作接口（actions）
- 组操作接口（actions）

> 说明：本次提案在 operator 仓库落地规范，server 侧需创建对应 change 并对齐参数透传。

## 风险与防护
- **核心风险：脑裂（Split-Brain）**
  - 跳过源集群缩零可能导致源、目标同时存在业务副本。
- **防护策略**
  - 参数默认关闭，必须显式传入才生效；
  - 记录 Event 与状态消息，确保可观测可审计；
  - 在文档与 API 描述中明确该参数仅用于受控场景（如应急处置/演练验证）。

## 影响范围
- `disaster-operator`
  - CRD Schema (`DisasterOperationSpec`)
  - Failover 步骤执行逻辑
  - Group 子操作创建透传
  - 相关单元/BDD 测试
- `disaster-server`（联动项）
  - 操作接口请求体解析与 CRD 透传
  - 接口测试与文档更新
