# Spec: AppBackup Paused State Synchronization

## Requirements

### Requirement: 同步暂停状态 (Sync Paused State)
`AppBackup` 的暂停状态必须 (MUST) 实时同步到其管理的 Velero `Schedule` 资源。

#### Scenario: 暂停 AppBackup
- **Given** 一个处于运行状态的 `AppBackup`，且关联了一个 Velero `Schedule`
- **When** 用户将 `AppBackup.Spec.Paused` 设置为 `true`
- **Then** Operator 应更新对应的 Velero `Schedule`，将其 `Spec.Paused` 设置为 `true`
- **And** 记录一个 Event 说明已暂停底层 Schedule

#### Scenario: 恢复 AppBackup
- **Given** 一个处于暂停状态的 `AppBackup` (`Paused=true`)
- **When** 用户将 `AppBackup.Spec.Paused` 设置为 `false`
- **Then** Operator 应更新对应的 Velero `Schedule`，将其 `Spec.Paused` 设置为 `false`
- **And** 记录一个 Event 说明已恢复底层 Schedule
