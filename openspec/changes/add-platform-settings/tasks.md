# Tasks: 系统设置（Key-Value 配置管理：name/config_key/value/remark）

## 执行状态说明（2026-03-10）

- 本次提案范围：**Server 端能力**（Key-Value 配置 CRUD、Base64 资产上传/读取、etcd 持久化）。
- 本仓库（`disaster-operator`）仅维护 OpenSpec 变更文档；实现预计落在 `disaster-server`（以及其部署清单）。
- 前端接入（配置管理页面、登录页展示）不在本次范围。
- 2026-03-11 已在 `disaster-server` 完成首版实现：`internal/apis/system_settings/v1` 与路由接入；剩余项见未勾选任务。

## 1. 口径冻结

- [x] 1.1 冻结配置项模型：`name/config_key/value/remark`（value 为字符串）。
- [x] 1.2 冻结存储载体：单例 ConfigMap（namespace/name/JSON schemaVersion + items map）。
- [x] 1.3 冻结 API：
  - [x] 管理：`GET/POST /api/v1/system-settings`、`PUT/DELETE /api/v1/system-settings/{config_key}`
  - [x] public：`GET /api/v1/system-settings/public?keys=...`（如需未登录展示）
  - [x] asset：`POST/GET /api/v1/system-settings/assets/{config_key}`
- [x] 1.4 冻结鉴权口径：仅 System Admin 可写；public 接口避免可枚举全部配置项。

## 2. etcd（ConfigMap）持久化

- [x] 2.1 在 Server 实现 `SystemSettingsStore`：基于 Kubernetes ConfigMap 的 `Get/Upsert/Update`（`data.settings` JSON）。
- [x] 2.2 实现默认值策略：ConfigMap 不存在时返回空列表或内置默认项（选择一种并固化）。
- [x] 2.3 实现并发控制：基于 `resourceVersion` 的冲突重试或 409 返回。
- [x] 2.4 实现 `config_key` 唯一性：创建/更新时不得产生重复 key。

## 3. CRUD API（管理员）

- [x] 3.1 `GET /api/v1/system-settings`：列表返回（可选支持 keys/q 过滤）；返回必须按 `config_key` 升序排序。
- [x] 3.2 `POST /api/v1/system-settings`：创建配置项（校验 config_key/name 长度与字符集）。
- [x] 3.3 `PUT /api/v1/system-settings/{config_key}`：更新 name/value/remark。
- [x] 3.4 `DELETE /api/v1/system-settings/{config_key}`：删除配置项。
- [x] 3.5 统一错误码与错误体（400/401/403/409/500），与现有 API 风格一致。

## 4. public 读取 API（可选）

- [x] 4.1 `GET /api/v1/system-settings/public?keys=...`：仅返回指定 key 的配置项（避免全量泄露）；返回必须按 `config_key` 升序排序。
- [ ] 4.2 明确哪些 keys 允许匿名读取（通过服务端配置或固定 allowlist）。

## 5. Base64 资产（asset）

- [x] 5.1 固化 Base64 规则：data URL 格式、MIME 白名单、大小上限（原始文件与编码后字符串）。
- [x] 5.2 `POST /api/v1/system-settings/assets/{config_key}`：上传文件 -> Base64 编码 -> 回写该配置项 value。
- [x] 5.3 `GET /api/v1/system-settings/assets/{config_key}`：从 value 解码并返回文件流。
- [x] 5.4 失败语义：编码失败或超限不得更新 ConfigMap（避免脏写入）。

## 6. 安全与权限

- [x] 6.1 鉴权中间件接入：写接口必须校验 System Admin。
- [x] 6.2（本阶段不做）新增部署 RBAC：默认沿用现有 Server SA 权限。
- [ ] 6.3（可选）补齐审计日志：记录更新 key、updatedBy、trace-id、来源信息。

## 7. 测试

- [ ] 7.1 单测覆盖：
  - [ ] `config_key` 格式/长度校验与唯一性
  - [ ] CRUD 行为与 409 冲突
  - [ ] asset 上传失败或超限不应更新 value
  - [x] Base64 value 非法格式时读取失败路径
- [ ] 7.2（可选）集成测试：Kind/EnvTest（上传 -> 下载 -> value 一致）。

## 8. 文档与验收

- [ ] 8.1 输出使用说明：如何创建配置项、public keys 用法、asset Base64 上传/读取、大小限制与格式约束。
- [x] 8.2 运行 `openspec validate add-platform-settings --strict` 并修复所有问题。
