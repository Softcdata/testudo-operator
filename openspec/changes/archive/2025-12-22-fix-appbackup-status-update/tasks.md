# Tasks

- [x] 修改 `internal/controller/appbackup/appbackup_controller.go` 中的 `Reconcile` 方法，修复状态更新判定逻辑。 <!-- id: fix-reconcile-logic -->
- [x] 验证修复后的控制器在集群不存在时的行为，确保 `status.phase` 更新为 `Pending` 且包含错误信息。 <!-- id: verify-fix -->
