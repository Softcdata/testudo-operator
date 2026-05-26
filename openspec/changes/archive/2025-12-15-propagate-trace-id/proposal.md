# Proposal: 传播 Trace ID

## Why
`disaster-server` 会在 CRD (如 `AppBackup`) 的 Annotations 中写入 `trace_id`，用于全链路追踪。目前 `disaster-operator` 在创建下游资源（如 Velero Backup, Velero Schedule）时，没有将这个 `trace_id` 传递下去，导致追踪链路中断。

## What Changes
本提案旨在修改 `disaster-operator` 的控制器逻辑，使其在创建子资源时，将父资源的 `trace_id` Annotation 传播到子资源中。

主要变更包括：
1.  **AppBackup Controller**:
    - 在创建 `Velero Backup` 时，将 `AppBackup` 的 `trace_id` 写入 `Velero Backup` 的 Annotations。
    - 在创建 `Velero Schedule` 时，将 `AppBackup` 的 `trace_id` 写入 `Velero Schedule` 的 Annotations。
    - 在创建 `Velero Schedule` 时，也将 `trace_id` 写入 `Schedule.Spec.Template.Metadata.Annotations`，以确保由 Schedule 创建的 Backup 也带有 `trace_id`。

受影响的组件：
- `internal/controller/appbackup/appbackup_controller.go`
