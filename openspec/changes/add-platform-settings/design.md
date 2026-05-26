# Design: 系统设置（Key-Value 配置管理：name/config_key/value/remark）

## 1. 总览

本设计提供一套轻量、可扩展的系统设置（配置管理）机制。它不尝试在 v1 引入强类型 schema，而是采用固定字段的 Key-Value 配置项模型：

- `name`：展示名称（可维护）
- `config_key`：配置键（唯一）
- `value`：配置值（字符串）
- `remark`：备注说明

设计目标：

1. **可扩展**：新增配置项无需改存储结构与数据库迁移，只需新增一条配置项。
2. **可维护**：配置项自带 name/remark，便于在 UI 管理与交付说明。
3. **不引入数据库**：配置存入 Kubernetes（最终落盘 etcd）。
4. **支持媒体资源**：二进制文件不走 S3，直接编码为 Base64 存入配置项 `value`；通过专用接口上传与读取。

## 2. 数据模型

### 2.1 SettingItem

```json
{
  "name": "平台名称",
  "config_key": "example.platformName",
  "value": "容灾平台",
  "remark": "导航栏/页面标题展示"
}
```

约束建议（v1 作为 Server 校验规则）：

- `config_key`：必填、全局唯一
  - 长度：1~128
  - 允许字符：字母数字、`.`、`-`、`_`
  - 建议使用点分命名（例如 `example.platformName`），但不强制
- `name`：必填，长度 1~64
- `value`：可为空字符串；
  - 普通文本建议上限 4096
  - Base64 资产值建议上限 350KB（结合 ConfigMap/etcd 容量约束）
- `remark`：可选，长度上限建议 1024

说明：

- `value` 统一为字符串：可用于简单文案、URL、数字字符串、JSON 字符串等。
- v1 不做强类型约束（例如 URL 校验、JSON 校验），由使用方与约定保证；后续若需要强校验，可通过独立提案增加 schema 扩展字段。
- 文档中的 `example.*` 仅为示例 key，不是内置保留字段，也不是写死配置项。

## 3. 存储设计（etcd）

### 3.1 载体选择

采用 ConfigMap 持久化（由 Kubernetes 写入 etcd）：

- Namespace：默认 `disaster-system`（可配置）
- Name：`disaster-platform-settings`
- Key：`data.settings`（JSON 文本）

### 3.2 ConfigMap JSON 结构

```json
{
  "schemaVersion": 1,
  "items": {
    "example.platformName": {
      "name": "平台名称",
      "config_key": "example.platformName",
      "value": "容灾平台",
      "remark": "导航栏/页面标题展示"
    },
    "example.logo": {
      "name": "平台 Logo",
      "config_key": "example.logo",
      "value": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAA...",
      "remark": "图片使用 data URL + base64，直接存 value"
    }
  },
  "updatedAt": "2026-03-10T12:00:00Z",
  "updatedBy": "system"
}
```

说明：

- `items` 采用 map（`config_key -> item`）以自然保证唯一性与便于更新。
- item 内部保留 `config_key` 字段是冗余的，但利于可读性与迁移调试。
- ConfigMap 不存在时，Server 应返回空列表或内置默认项（两者选一并固化口径；推荐返回空列表 + 前端自行提供默认展示，或由 Server 注入默认项）。

### 3.3 并发控制

- 读取：`Get(ConfigMap)`；NotFound 则按默认策略返回。
- 写入：使用 Kubernetes `resourceVersion` 做乐观锁控制：
  - 读取 -> 修改 -> Update
  - 冲突时重试有限次数；超过上限返回 409

## 4. 媒体资源（Base64）

### 4.1 编码格式

- 统一格式：`data:<mime>;base64,<payload>`
- MIME 白名单建议：`image/png`、`image/jpeg`（SVG 是否支持单独决策）
- 大小限制建议：
  - 原始文件 `<= 256KB`
  - 编码后字符串 `<= 350KB`

### 4.2 上传接口策略

`POST /api/v1/system-settings/assets/{config_key}`（System Admin）：

1. 接收 `multipart/form-data` 文件（字段名建议 `file`）
2. 校验：
   - 文件大小限制（建议 ≤ 256KB）
   - Content-Type 白名单（建议 `image/png`, `image/jpeg`；SVG 是否允许由策略决定）
3. 编码为 Base64 data URL
4. 更新对应配置项：
   - 若配置项不存在：可选择自动创建（name 取 `config_key`，remark 为空），或返回 404（需固化 v1 口径）
   - `value` 写入 Base64 data URL

### 4.3 读取/下载策略

`GET /api/v1/system-settings/assets/{config_key}`：

1. Server 读取配置项 `value`
2. 解析 `data:<mime>;base64,<payload>`
3. 解码并以对应 `Content-Type` 返回文件流

若 `value` 不是合法 Base64 data URL，返回 400 或 422（按现有错误码规范统一）。

## 5. API 设计

### 5.1 管理接口（管理员）

- `GET /api/v1/system-settings`
  - 返回全量配置项列表（`name/config_key/value/remark`）
  - 返回列表必须按 `config_key` 升序排序（保证稳定性）
  - 支持可选查询：`?q=`（按 name/config_key 模糊），`?keys=`（逗号分隔）
- `POST /api/v1/system-settings`
  - 创建配置项（`config_key` 必须唯一）
- `PUT /api/v1/system-settings/{config_key}`
  - 更新配置项（name/value/remark）
- `DELETE /api/v1/system-settings/{config_key}`
  - 删除配置项

### 5.2 public 读取接口（可选）

为支持未登录页面展示（例如平台名称/Logo），提供：

- `GET /api/v1/system-settings/public?keys=example.platformName,example.logo`
  - 仅返回指定 keys 的配置项（避免公开枚举全部配置）
  - 返回列表必须按 `config_key` 升序排序（保证稳定性）

同时可按产品需要开放：

- `GET /api/v1/system-settings/assets/{config_key}` 的匿名访问（仅用于公开展示的 asset key）

## 6. 权限与安全

- 写接口（POST/PUT/DELETE/上传）必须要求 System Admin。
- public 接口必须避免“可枚举全部配置项”的设计（推荐按 keys 查询）。
- 明确禁止在系统设置中存储敏感密钥（Non-Goals），并在文档/代码层面做防误用提示。
- 本阶段不新增 RBAC 资源，默认沿用现有 Server ServiceAccount 权限。

## 7. 可观测性与审计

- ConfigMap 顶层记录 `updatedAt/updatedBy`。
- 额外审计建议：
  - Server 记录结构化日志：操作类型、config_key 列表、调用人、trace-id、来源 IP。

## 8. 测试建议

- 单测：
  - `config_key` 唯一性、格式校验
  - CRUD 行为与并发冲突（resourceVersion）处理
  - asset 上传失败不应更新 value
  - Base64 非法格式或超限值处理
- 集成测试（可选）：
  - Kind/EnvTest：上传 asset -> 读取 asset -> 配置项 value 更新一致
