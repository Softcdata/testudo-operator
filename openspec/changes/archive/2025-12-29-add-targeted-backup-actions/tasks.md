# Tasks: 增强手动动作支持指定备份和删除操作

## 0. 测试设计（测试先行）
- [x] 0.1 设计 `Delete` 动作的 BDD 测试场景
  - [x] 成功删除指定备份
  - [x] 删除不存在的备份
  - [x] 删除正在进行的备份（应阻止）
- [x] 0.2 设计 `TargetBackup` 字段的 BDD 测试场景
  - [x] 指定备份名称进行 Retry
  - [x] 不指定时默认操作最新备份（向后兼容）
- [x] 0.3 在 `appbackup_controller_test.go` 中实现测试用例框架

## 1. API 变更
- [x] 1.1 修改 `pkg/apis/disaster/v1/appbackup_types.go`
  - [x] 在 `BackupAction` 中添加 `TargetBackup string` 字段
  - [x] 在 `Type` 注释中添加 `"Delete"` 为有效值
- [x] 1.2 执行 `make generate` 和 `make manifests` 更新生成代码

## 2. Handler 实现
- [x] 2.1 在 `ready_handler.go` 或新建 `action_handler.go` 中实现 Delete 逻辑
  - [x] 2.1.1 验证目标备份存在
  - [x] 2.1.2 验证备份不在进行中状态
  - [x] 2.1.3 调用远程集群删除 Velero Backup
  - [x] 2.1.4 更新 `Status.History` 移除该记录
  - [x] 2.1.5 更新 `Status.TotalBackups` 统计
- [x] 2.2 修改 Retry 逻辑支持 `TargetBackup` 字段
  - [x] 2.2.1 如果 `TargetBackup` 为空，使用最新备份（当前行为）
  - [x] 2.2.2 如果指定了 `TargetBackup`，验证并操作该备份
- [x] 2.3 修改 Cancel 逻辑支持 `TargetBackup` 字段

## 3. 测试与验证
- [x] 3.1 运行 BDD 测试并确保全部通过
- [x] 3.2 验证向后兼容性（不传 TargetBackup 时行为不变）
- [x] 3.3 验证测试覆盖率 >= 80%

## 4. 上游同步提醒
- [x] 4.1 通知 disaster-server 团队 API 变更（`BackupAction.TargetBackup` 新增）
- [x] 4.2 更新相关的 OpenAPI/Swagger 文档（如有）

## 5. Bug Fixes
- [x] 5.1 修复删除最后一个备份后触发 Initial Backup 自动重建的问题
  - [x] 修改 `appbackup_ready.go` 初始备份判断条件
  - [x] 修改 `Delete` 动作逻辑：将记录标记为 `Deleted` 而非移除
  - [x] 添加回归测试用例 `should NOT recreate initial backup...`
