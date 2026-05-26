## 1. Spec & Design
- [x] 1.1 更新 `global-events` 规范，定义 `Timeline` 聚合规则和 `Progress` 上报标准

## 2. Disaster Operator Implementation
- [x] 2.1 修改 `pkg/helper/event_reporter.go`
  - [x] 实现 `ReportTaskProgress` 方法
  - [x] 确保 `InProgress` 事件不携带 EndTime 但更新 Message
- [x] 2.2 在关键 Controller 中集成进度上报 (示例)
  - [x] `AppBackup` Controller: 上报 Velero Backup 创建成功、等待完成等状态
  - [x] `Cluster` Controller: 上报连接检查进度
  - [x] `AppRestore` Controller: 上报 Velero Restore 创建成功、等待执行、执行中状态
  - [x] `StorageRepository` Controller: 上报 S3 连接和验证进度

## 3. Disaster Server Implementation
- [x] 3.1 修改 `internal/apis/event/v1/types.go`
  - [x] 更新 `TaskEvent` 结构体，增加 `Timeline []EventNode` 字段
- [x] 3.2 修改 `internal/apis/event/v1/list.go`
  - [x] 重构 `aggregateEvents` 逻辑
  - [x] 遍历事件时，将所有相关事件记录入 `Timeline`
  - [x] 按时间对 `Timeline` 进行排序

## 4. Verification
- [x] 4.1 编写/更新 Operator 单元测试 (`global_event_test.go`)
- [x] 4.2 验证 Server API 返回的 JSON 包含正确的 `timeline` 数据
