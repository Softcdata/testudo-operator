## MODIFIED Requirements

### Requirement: 资源标识与隔离 (Resource Identification & Isolation)
为了确保控制器能够精确管理属于特定 `AppBackup` 实例的资源，并避免同名资源冲突，必须 (MUST) 采用双标签策略。

#### Scenario: 双标签机制
- **Given** 一个 AppBackup 资源
- **When** 控制器创建关联的 Velero 资源
- **Then** 必须 (MUST) 添加 `testudo.softcdata.com/app-backup` (Name) 和 `testudo.softcdata.com/app-backup-uid` (UID) 标签。

## ADDED Requirements

### Requirement: 内部架构 (Internal Architecture)
为了保证代码的可维护性和扩展性，控制器必须 (MUST) 采用模块化的设计。

#### Scenario: 状态机模式
- **Given** AppBackup 控制器的复杂业务逻辑
- **When** 执行 Reconcile 循环
- **Then** 必须 (MUST) 通过状态机（State Machine）模式按顺序执行各个阶段（Init, Finalizer, Storage, Action, Schedule/OneOff, Status）。
