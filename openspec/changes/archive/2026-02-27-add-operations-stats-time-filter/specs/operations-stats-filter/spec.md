# 操作统计时间过滤规范

## 目的
为客户端提供一种机制，用于检索按特定时间窗口（如今天、本周、本月）过滤的容灾实例操作统计数据，提供针对性的操作数据可视化。

## ADDED Requirements

### Requirement: 支持时间过滤的操作统计接口
系统 MUST 提供一个新的 API 端点 `GET /operations/by-time`，该接口基于 `BackupRestoreStatistics` CR，通过对资源的 `creationTimestamp` 应用时间过滤，返回聚合后的操作统计数据。

#### Scenario: 按“今天”过滤操作
- **给定** 一个运行中的容灾服务端（disaster server API）
- **当** 用户发起 `GET` 请求到 `/operations/by-time` 并附带参数 `period=today`（或类似的预定义过滤条件如 `week`、`month`）
- **那么** 系统必须检索所有匹配标签 `testudo.softcdata.com/owner-kind=DisasterOperation` 且其 `creationTimestamp` 落在指定相对时间约束内（例如当前日历日，从午夜到当前时间）的 `BackupRestoreStatistics` 资源。
- **并且** 系统必须聚合这些过滤后的记录并响应 `StatisticsDTO` 数据。

#### Scenario: 按显式时间范围过滤操作
- **给定** 一个运行中的容灾服务端（disaster server API）
- **当** 用户发起 `GET` 请求到 `/operations/by-time` 并附带采用 RFC3339 格式的明确参数 `startTime` 和 `endTime`
- **那么** 系统必须检索操作的 `creationTimestamp` 介于 `startTime`（含）和 `endTime`（含）之间的所有匹配操作。
- **并且** 系统必须聚合这些过滤后的记录并返回 `StatisticsDTO` 响应数据。
