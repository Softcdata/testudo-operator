# Tasks: 追踪资源生命周期与变更历史事件

- [ ] **Specs Update**: 更新 `app-backup` 和 `app-restore` 规范，强制要求生命周期事件。 <!-- id: spec-update -->
- [ ] **AppBackup Create/Delete Events**: 在 `AppBackup` 控制器中实现创建和删除事件上报。 <!-- id: appbackup-lifecycle -->
- [ ] **AppBackup Update Events**: 在 `AppBackup` 控制器中检测 Spec 变更并上报更新事件。 <!-- id: appbackup-update -->
- [ ] **AppRestore Lifecycle Events**: 在 `AppRestore` 控制器中实现创建和删除事件上报。 <!-- id: apprestore-lifecycle -->
- [ ] **Validation**: 部署并验证事件是否正确生成并能在 K8s Events 中查看到。 <!-- id: validation -->
