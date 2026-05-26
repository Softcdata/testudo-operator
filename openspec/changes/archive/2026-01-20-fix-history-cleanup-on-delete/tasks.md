# Tasks: 修复 AppBackup 删除操作历史去重

- [ ] **Spec Update**: 更新 AppBackup 规范，明确删除操作和状态映射的预期行为。 <!-- id: spec-update -->
- [ ] **Update Status Mapping**: 修改 `getManagedStatus`，将 `Deleting` Phase 映射为独立状态而非 `Canceled`。 <!-- id: update-status-mapping -->
- [ ] **Verify Cleanup**: 验证当 K8s 资源消失时，`Deleting` 状态的记录能被正确清理。 <!-- id: verify-cleanup -->
