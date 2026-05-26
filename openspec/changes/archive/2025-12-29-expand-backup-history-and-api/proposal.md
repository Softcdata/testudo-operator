# Change: 扩充备份历史信息与优化 API 接口

## Why
目前 `AppBackup` 的 `Status.History` 字段仅包含简要信息，缺乏 Velero 备份的详细状态（如快照卷信息、钩子状态等），导致前端无法展示完整的历史记录详情。同时，随着历史记录增加，列表接口 (`GET /appbackups`) 返回的数据包过大，影响性能。

## What Changes
1.  **Operator**:
    - 修改 `BackupRecord` 结构体，增加 `VeleroStatus` 字段，存储完整的 Velero Backup Status。
    - 更新 Reconcile 逻辑，在同步状态时填充该字段。

2.  **Server**:
    - 优化 `AppBackup` 列表接口 (`GET /appbackups`)，返回的 DTO 中隐藏 `History` 字段。
    - 保持 `AppBackup` 详情接口 (`GET /appbackups/:name`) 展示完整 `History`。
    - 新增独立接口 `GET /appbackups/:name/history`，专门用于查询指定应用的备份历史列表。

## Impact
- **Operator**: CRD schema 变更（字段增加），Controller 状态同步逻辑微调。
- **Server**: API 契约变更（List 接口瘦身），新增路由。
- **Frontend**: 需要适配新的 API 调用方式（列表页不展示历史，详情页或独立历史页请求新接口）。

## 关键测试场景
1.  **Operator**: 验证 History 中包含了完整的 Velero Status 信息。
2.  **Server**: 验证 List 接口不返回 History。
3.  **Server**: 验证 Detail 接口返回完整 History。
4.  **Server**: 验证 History 子资源接口返回正确的数据列表。
