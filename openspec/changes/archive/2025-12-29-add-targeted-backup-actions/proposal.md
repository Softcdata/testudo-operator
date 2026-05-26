# Change: 增强手动动作支持指定备份和删除操作

## Why
目前 AppBackup 的手动动作（Cancel、Retry）默认作用于最新的 Velero 备份，用户无法对历史备份进行操作。此外，用户需要能够删除不再需要的 Velero 备份以释放存储空间。本变更将：
1. 允许用户指定要操作的 Velero 备份名称
2. 新增 `Delete` 动作类型，支持删除指定的 Velero 备份

## What Changes
- **BREAKING**: 修改 `BackupAction` 结构体，添加 `TargetBackup` 字段
- 新增 `Delete` 动作类型
- 修改 `Retry` 和 `Cancel` 逻辑，支持指定备份名称（可选，默认仍为最新备份）
- 更新 Handler 逻辑以支持新字段

## Impact
- 受影响的规范：`specs/app-backup`
- 受影响的代码：
  - `pkg/apis/disaster/v1/appbackup_types.go` - 修改 `BackupAction` 结构体
  - `internal/controller/appbackup/handlers/ready_handler.go` - 处理 Delete 动作
  - `internal/controller/appbackup/handlers/action_handler.go` - 支持指定备份
- 上游影响：**disaster-server** 需要同步更新 API 以支持传递 `TargetBackup` 参数

## 关键测试场景
1. 指定备份名称进行 Retry 操作
2. 指定备份名称进行 Delete 操作
3. 不指定备份名称时默认操作最新备份（向后兼容）
4. 删除不存在的备份返回适当错误
5. 删除正在进行的备份应被阻止
