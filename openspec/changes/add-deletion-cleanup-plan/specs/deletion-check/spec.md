## ADDED Requirements

### Requirement: 删除检查必须返回 cleanup plan

统一删除检查接口必须 (MUST) 在保留现有 `upstream/downstream/can_delete` 语义的前提下，返回删除影响面的清理计划。

#### Scenario: 返回 finalizer 清理计划
- **Given** 一个删除时会在 Finalizer 中清理外部资源的对象（例如 `AppBackup` 或 `AppRestore`）
- **When** 客户端调用 `POST /api/v1/deletion/check`
- **Then** 响应必须 (MUST) 包含 `cleanup_plan.finalizer_cleanup[]`
- **And** 每个计划项必须 (MUST) 说明资源类型与 `relation_code`

#### Scenario: 返回级联删除计划（OwnerReference/子资源）
- **Given** 一个删除时会依赖 `OwnerReference` 或明确子资源关系进行级联清理的对象（例如 `DisasterInstance`/`DisasterGroup`/`DisasterDrill`）
- **When** 客户端调用 `POST /api/v1/deletion/check`
- **Then** 响应必须 (MUST) 包含 `cleanup_plan.cascade_cleanup[]`
- **And** 这些资源必须 (MUST) 与阻塞删除的 `upstream` 语义区分开

#### Scenario: 子资源不作为 upstream 阻塞
- **Given** 目标资源存在子资源（例如 `DisasterInstance`/`DisasterGroup` 的 `DisasterOperation`，或 `DisasterDrill` -> `DisasterOperation`）
- **When** 客户端调用 `POST /api/v1/deletion/check`
- **Then** 响应中的 `upstream` 不得包含这些子资源
- **And** `cleanup_plan.cascade_cleanup[]` 必须包含这些子资源

#### Scenario: 上游资源必须保留在 upstream
- **Given** 目标资源存在上游引用方（例如 `DisasterOperation` 的 `DisasterDrill`）
- **When** 客户端调用 `POST /api/v1/deletion/check`
- **Then** 响应中的 `upstream` 必须包含这些上游资源

#### Scenario: 无连带清理对象时保持兼容
- **Given** 一个没有 Finalizer 清理对象且没有级联子资源的目标资源
- **When** 客户端调用 `POST /api/v1/deletion/check`
- **Then** 响应必须 (MUST) 仍包含 `cleanup_plan`
- **And** `cleanup_plan.has_cleanup` 必须 (MUST) 为 `false`
- **And** `finalizer_cleanup[]` 与 `cascade_cleanup[]` 必须 (MUST) 为空数组

### Requirement: Finalizer 管理的外部资源必须具备通用 cleanup 标签

所有由 Controller 在 Finalizer 中显式清理的资源必须 (MUST) 具备统一 cleanup 标签，以便 Server 在删除检查时统一查询。

#### Scenario: AppBackup 管理的 Velero 资源写入 cleanup 标签
- **Given** `AppBackup` 创建或更新其关联的 Velero `Schedule` 或 `Backup`
- **When** Controller 写入外部资源元数据
- **Then** 这些资源必须 (MUST) 带有 `testudo.softcdata.com/cleanup-owner-token`
- **And** 必须 (MUST) 带有 `testudo.softcdata.com/cleanup-relation`
- **And** 必须 (MUST) 带有 `testudo.softcdata.com/cleanup-strategy`

#### Scenario: AppRestore 管理的外部资源写入 cleanup 标签
- **Given** `AppRestore` 创建或更新其关联的 Velero `Restore` 或 ResourceModifier `ConfigMap`
- **When** Controller 写入外部资源元数据
- **Then** 这些资源必须 (MUST) 带有统一 cleanup 标签
- **And** owner token 必须 (MUST) 复用 `AppRestore` 的 `dependency-token`

#### Scenario: 目标集群不可达时返回 unresolved cleanup 项
- **Given** 一个目标资源存在 Finalizer 清理对象，但对应目标集群当前不可达
- **When** 客户端调用 `POST /api/v1/deletion/check`
- **Then** 接口必须 (MUST) 仍返回对应 cleanup 项
- **And** 该项必须 (MUST) 标记为 `resolved=false`
- **And** 该项必须 (MUST) 给出 cluster 或 selector 信息用于解释
