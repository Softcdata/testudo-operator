## ADDED Requirements

### Requirement: AppBackup 必须维护来源分类标签
`AppBackup` 控制器必须 (MUST) 在资源标签中维护统一的来源分类信息，用于区分用户创建与容灾实例同步创建的备份任务。

#### Scenario: DataSync 创建的 AppBackup 应标记为实例来源
- **Given** 一个 `AppBackup` 的 controller owner 为 `DataSync`
- **When** 控制器执行标签同步
- **Then** 必须 (MUST) 设置 `testudo.softcdata.com/app-resource-origin=disaster-instance`
- **And** 必须 (MUST) 设置 `testudo.softcdata.com/app-resource-owner-kind=datasync`
- **And** 必须 (MUST) 设置 `testudo.softcdata.com/app-resource-owner-name=<DataSyncName>`

#### Scenario: ResourceSync 创建的 AppBackup 应标记为实例来源
- **Given** 一个 `AppBackup` 的 controller owner 为 `ResourceSync`
- **When** 控制器执行标签同步
- **Then** 必须 (MUST) 设置 `testudo.softcdata.com/app-resource-origin=disaster-instance`
- **And** 必须 (MUST) 设置 `testudo.softcdata.com/app-resource-owner-kind=resourcesync`
- **And** 必须 (MUST) 设置 `testudo.softcdata.com/app-resource-owner-name=<ResourceSyncName>`

#### Scenario: 用户创建的 AppBackup 应标记为用户来源
- **Given** 一个 `AppBackup` 没有 `DataSync/ResourceSync` controller owner
- **When** 控制器执行标签同步
- **Then** 必须 (MUST) 设置 `testudo.softcdata.com/app-resource-origin=user`
- **And** 必须 (MUST) 设置 `testudo.softcdata.com/app-resource-owner-kind=user`
- **And** 必须 (MUST) 删除 `testudo.softcdata.com/app-resource-owner-name`

#### Scenario: 存量 AppBackup 必须可自动回填来源标签
- **Given** 一个历史 `AppBackup` 缺少来源标签
- **When** 控制器执行 Reconcile
- **Then** 必须 (MUST) 按 owner 判定规则自动补齐来源标签
- **And** 不得 (MUST NOT) 破坏现有 `app-backup-*` 搜索标签语义
