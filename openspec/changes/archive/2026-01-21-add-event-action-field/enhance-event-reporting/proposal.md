# 提案：增强事件报告能力

## 背景
`disaster-server` 正在引入历史事件列表功能，该功能依赖于 Operator 发射的结构化事件。当前 Operator 的事件发射较为零散，且缺乏诸如“耗时”和“触发人”等关键审计信息。

## 目标
1. 规范 `AppBackup` 和 `AppRestore` 的事件发射。
2. 在任务完成时记录执行耗时。
3. 在事件消息中包含触发人信息（如果可用）。
4. 确保事件 Reason 语义明确，便于上游 API 筛选。

## 范围
- `internal/controller/appbackup/`
- `internal/controller/apprestore/`

## 变更 ID
`enhance-event-reporting`
