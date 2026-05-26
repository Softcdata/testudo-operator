# 提案: 增加容灾操作统计时间过滤 API

## 现状上下文
目前，系统提供了一个 API 端点 (`/operations`)，用于检索系统中所有容灾实例操作的聚合统计信息。为了提供更细粒度的展示，现在需要添加一个新接口，能够根据时间段（例如今天、本周或本月的操作）对这些统计数据进行过滤。
底层数据存储为具有标签 `testudo.softcdata.com/owner-kind=DisasterOperation` 的 `BackupRestoreStatistics` 自定义资源 (CRD)。

## 提议的变更
1. **新增 API 端点**: 在 `internal/apis/statistics/v1/router.go` 中添加 `GET /operations/by-time` 路由。
2. **Handler 实现**: 在 `internal/apis/statistics/v1/handler.go` 中实现 `GetOperationStatisticsByTime(c context.Context, ctx *app.RequestContext)`。
3. **过滤逻辑**: 
   - 从请求上下文解析时间范围参数。支持语义化 `period` 参数（如 `today`、`week`、`month`）或明确的 `startTime` 和 `endTime` 时间戳（RFC3339 格式）。
   - 获取所有匹配 `testudo.softcdata.com/owner-kind=DisasterOperation` 的 `BackupRestoreStatistics` 资源。
   - 根据它们的 `metadata.creationTimestamp` 与指定时间范围进行对比评估，过滤出符合条件的结果。
4. **响应格式**: 使用现有的 `StatisticsDTO` 聚合结果并将其返回。

## 设计依据
通过添加专用路由来保证现有的 `/operations` 路由的签名及行为得以原样保持，同时增加专门的时间维度聚合。使用 `creationTimestamp` 是确定操作对应的统计数据进入系统的最佳评估方式。
