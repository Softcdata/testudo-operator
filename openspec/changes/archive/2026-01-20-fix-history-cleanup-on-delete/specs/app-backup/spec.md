## MODIFIED Requirements
### Requirement: 备份状态映射
控制器必须 (MUST) 准确反映 Velero 备份的当前状态，特别是在生命周期结束阶段。

#### Scenario: Deleting 状态映射
- **Given** 一个处于 `Deleting` 阶段的 Velero 备份
- **When** 控制器同步其状态到 `AppBackup` History
- **Then** `ManagedStatus` 必须 (MUST) 设置为 `Deleting`，而不是 `Canceled`
- **And** 当该备份在 K8s 中消失时，History 记录必须 (MUST) 被自动清理

### Requirement: 历史记录清理
除了明确标记为 `Canceled` 的操作历史外，其他已在集群中不存在的备份记录必须 (MUST) 被移除。

#### Scenario: 删除操作清理
- **Given** 用户对某个备份执行了 `Delete` Action
- **When** 备份彻底从 K8s 集群中删除
- **Then** `AppBackup` History 中不应再包含该备份记录
