# Tasks: Operations Statistics Time Filter API

- [x] Parse time filtering parameters (`startTime`/`endTime` or `period` enum like `today`/`week`/`month`) from the request context.
- [x] Implement `GetOperationStatisticsByTime` in `disaster-server/internal/apis/statistics/v1/handler.go` that retrieves `BackupRestoreStatistics` and filters them based on their `metadata.creationTimestamp`.
- [x] Aggregate the filtered statistics into the `StatisticsDTO`.
- [x] Add the route mapping for `GET /operations/by-time` in `disaster-server/internal/apis/statistics/v1/router.go`.
- [x] Write integration or unit tests validating the time-bounded filtering.
