## CRD 定义变更
- [x] 修改 `pkg/apis/disaster/v1/resourcesync_types.go`: 定义 `SyncHistoryRecord` 结构体并添加到 `ResourceSyncStatus`. <!-- id: defines-history-record -->
- [x] 修改 `pkg/apis/disaster/v1/datasync_types.go`: 将 `History` 字段添加到 `DataSyncStatus`. <!-- id: update-datasync-status -->
- [x] 运行 `make manifests` 和 `make generate` 更新 CRD 和 DeepCopy 代码. <!-- id: update-generated-code -->

## Controller 逻辑实现
- [x] `ResourceSync` Controller: 在同步流程结束（备份和恢复完成）时，构建 `SyncHistoryRecord` 并追加到 Status.History. <!-- id: resourcesync-append-history -->
- [x] `DataSync` Controller: 在同步流程结束时，构建 `SyncHistoryRecord` 并追加到 Status.History. <!-- id: datasync-append-history -->
- [x] 实现 History 裁剪逻辑（保留最近 20 条记录）. <!-- id: implement-history-pruning -->
- [x] 实现统计同步逻辑：根据 History 列表计算 Total/Completed/Failed，并更新关联的 `BackupRestoreStatistics` CR. <!-- id: sync-stats-logic -->

## 服务端适配
- [x] 更新 `disaster-server` 中的 DTO，使其包含 History 字段（可选，取决于是否需要展示列表）. <!-- id: update-server-dto -->
- [x] 验证 `sync-status` 接口返回的统计数据是否准确反映了新的逻辑. <!-- id: verify-server-stats -->

## 测试验证
- [ ] 编写/更新 E2E 测试，验证 History 列表的生成和裁剪. <!-- id: e2e-test-history -->
- [ ] 验证 `BackupRestoreStatistics` 是否正确更新. <!-- id: verify-stats-crd -->
