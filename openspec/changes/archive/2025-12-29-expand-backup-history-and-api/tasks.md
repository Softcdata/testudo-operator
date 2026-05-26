# Tasks: 扩充备份历史信息与优化 API 接口

## 0. 测试设计
- [ ] 0.1 Operator BDD 测试
  - [ ] 验证 `syncStatus` 能正确填充 `VeleroStatus`
- [ ] 0.2 Server API 测试
  - [ ] 设计 `GET /appbackups` 返回结果校验（不含 history）
  - [ ] 设计 `GET /appbackups/:name/history` 测试

## 1. Operator 变更
- [ ] 1.1 修改 `pkg/apis/disaster/v1/appbackup_types.go`
  - [ ] 在 `BackupRecord` 中添加 `VeleroStatus` 字段
- [ ] 1.2 执行 `make generate` 和 `make manifests`
- [ ] 1.3 更新 `internal/controller/appbackup/appbackup_ready.go`
  - [ ] 在状态同步时，将 `veleroBackup.Status` 赋值给 `BackupRecord.VeleroStatus`
- [ ] 1.4 运行 Operator 测试确保通过

## 2. Server 变更
- [ ] 2.1 修改 `internal/apis/app_backup/v1/types.go`
  - [ ] 定义 `AppBackupListDTO` 或调整 `ConvertToAppBackupDTO` 逻辑以支持不同视图（List vs Detail）
- [ ] 2.2 修改 `internal/apis/app_backup/v1/handler.go`
  - [ ] `appBackups` (List) 接口：转换时不填充 History
  - [ ] 实现 `getBackupHistory` (GET /:name/history) 接口
- [ ] 2.3 注册新路由
- [ ] 2.4 运行 Server 测试确保 API 行为符合预期

## 3. 验证与归档
- [ ] 3.1 部署 Operator 和 Server
- [ ] 3.2 手动验证 API 响应结构
- [ ] 3.3 归档变更
