# AppBackup Lifecycle Delta

## ADDED Requirements

### Requirement: Trace ID 传播 (Trace ID Propagation)
为了支持全链路追踪，控制器必须 (MUST) 将 `AppBackup` 上的 `trace_id` Annotation 传播到所有创建的子资源中。

#### Scenario: 传播到 Velero Backup
- **Given** 一个带有 `testudo.softcdata.com/trace-id` Annotation 的 `AppBackup`
- **When** 控制器创建关联的 `Velero Backup`
- **Then** 该 `Velero Backup` 的 Annotations 中必须 (MUST) 包含相同的 `trace_id`

#### Scenario: 传播到 Velero Schedule
- **Given** 一个带有 `testudo.softcdata.com/trace-id` Annotation 的 `AppBackup`
- **When** 控制器创建关联的 `Velero Schedule`
- **Then** 该 `Velero Schedule` 的 Annotations 中必须 (MUST) 包含相同的 `trace_id`
- **And** 该 `Velero Schedule` 的 `Spec.Template.Metadata.Annotations` 中也必须 (MUST) 包含相同的 `trace_id` (以确保由 Schedule 创建的 Backup 也继承该 ID)
