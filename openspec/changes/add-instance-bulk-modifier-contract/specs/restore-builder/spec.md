## ADDED Requirements

### Requirement: Restore Builder 必须透传已启用批量修改来源摘要
系统必须 (MUST) 在恢复构建产物中透传已启用批量修改来源摘要，以支持审计与排障。

#### Scenario: AppRestore 写入批量来源摘要
- **Given** 一个 `DisasterInstance` 的 `restorePolicy` 包含至少一个已启用 `bulkModifierAction`
- **And** 已写入 `modifierRuleSnapshotHash`
- **When** Restore Builder 生成 `AppRestore`
- **Then** `AppRestore` 必须写入批量来源摘要或等价注解
- **And** 必须至少写入：
  - `testudo.softcdata.com/modifier-source=bulkActions`
  - `testudo.softcdata.com/modifier-bulk-action-count=<已启用动作数>`
  - `testudo.softcdata.com/modifier-snapshot-hash`

#### Scenario: 全部 bulk 动作禁用时不写批量摘要
- **Given** 一个 `DisasterInstance` 的 `restorePolicy.bulkModifierActions` 全部为 `enabled=false`
- **When** Restore Builder 生成 `AppRestore`
- **Then** 系统不得写入 bulk 来源摘要
- **And** 必须保持与无 bulk 动作时一致的编译行为

#### Scenario: 无批量动作时保持旧行为
- **Given** 一个 `DisasterInstance` 的 `restorePolicy` 没有 `bulkModifierActions`
- **When** Restore Builder 生成 `AppRestore`
- **Then** 系统不得因为缺少批量摘要而改变原有规则编译行为
