# Change: Refactor AppBackup Controller to State Machine

## Why
目前 `AppBackup` 的 `Reconcile` 方法逻辑过于复杂，包含大量的 `if-else` 分支和嵌套逻辑，混合了资源验证、Finalizer 处理、手动 Action 处理、定时任务管理和一次性备份逻辑。这使得代码难以阅读、维护和扩展。

## What Changes
- 引入状态机（State Machine）模式重构 `AppBackup` 控制器。
- 将核心逻辑拆分为独立的状态/阶段处理器（Handlers）。
- 封装一个通用的状态机组件，管理状态流转。
- **保证现有业务逻辑和行为不变**。

## Impact
- **Affected Specs**: `app-backup-lifecycle` (仅涉及内部实现架构，不改变外部行为)
- **Affected Code**: `internal/controller/appbackup_controller.go` 及相关辅助文件。
