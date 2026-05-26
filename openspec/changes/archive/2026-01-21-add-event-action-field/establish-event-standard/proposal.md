# 提案：建立全局结构化事件标准

## 背景
我们已经在 V1 阶段成功为 `Cluster` 和 `StorageRepository` 资源实现了结构化事件上报。为了确保未来迁移（`AppBackup`, `AppRestore`）和 V2 开发（`DataSync`, `ResourceSync`）的一致性，我们需要制定一份正式的规范文档。

## 目标
在 `disaster-operator` 项目中建立事件上报的全局标准。
更新现有的开发规范文档 `openspec/specs/development-standards/spec.md`，使其成为事件格式和实现模式的单一事实来源 (Single Source of Truth)。

## 设计详情

我们将更新现有的 `openspec/specs/development-standards/spec.md`，包含以下内容：

1.  **强制 Label**: 明确要求必须包含 `testudo.softcdata.com/task-event: "true"`。
2.  **防抖机制 (Debouncing)**: 增加在 CRD Status 中使用 `LastEventPhase` 以防止重复事件的要求。
3.  **完善消息格式**: 更新消息格式规范，增加 `[TraceID: %s]` 字段。
4.  **Helper 使用**: 强制要求使用 `pkg/helper` 包中的函数。

### 提议的文档内容
(我们将追加或修改 `openspec/specs/development-standards/spec.md` 中的 Requirements)

## 位置
目标文件: `openspec/specs/development-standards/spec.md`
(集成到现有的开发标准中)

## 影响
- 本提案不涉及代码变更。
- 更新现有文档。
