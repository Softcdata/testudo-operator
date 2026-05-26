# Change: 添加系统设置（Key-Value 配置管理：可扩展、可维护）

## Why

目前平台级配置（例如平台名称、Logo、favicon、帮助链接、页脚文案、功能开关等）缺少统一、可维护的管理方式，常见问题包括：

- 配置写死在前端/后端代码里，改动需要发版；
- 多环境（测试/预发/生产）品牌与文案不同，缺少一套可复用的配置机制；
- 配置项持续增加后，缺乏统一的约束与审计，容易“越配越乱”。

因此需要提供一个**系统设置（配置管理）**能力，允许系统管理员在运行时维护配置项。

本提案采用轻量模型：**Key-Value 配置项列表**，每条配置项包含：

- `name`：展示名称（可配置）
- `config_key`：配置键（可配置且唯一）
- `value`：配置值（字符串）
- `remark`：备注说明（可配置）

存储策略保持不引入新数据库：

- 配置项存入 Kubernetes 的持久化存储层（即 etcd，落地方式为 ConfigMap 等资源）。
- 媒体资源（图片等二进制）不走 S3，直接以 Base64 文本存入配置项 `value`。

## Goals

- 提供 Server 侧接口，支持系统设置的增删改查（CRUD）。
- 配置项模型固定为 `{ name, config_key, value, remark }`，支持后续新增任意配置项而不改存储结构。
- 配置项持久化到 etcd（通过 Kubernetes API 写入 ConfigMap）。
- 支持媒体资源以 Base64 存储：
  - Server 提供上传接口，接收文件后编码为 Base64；
  - 将编码后的字符串写入对应配置项的 `value`；
  - 提供读取/下载入口，由 Server 从 `value` 解码后返回文件流。
- 权限受控：仅 System Admin 可写（创建/更新/删除/上传），读取接口可按产品需要开放（至少支持未登录场景读取平台名称与 Logo）。
- 对更新行为提供可追溯性（`updatedAt/updatedBy`）。

## Non-Goals

- 不在 v1 中引入强类型 schema（例如为每个 key 定义 type/default/constraints）；`value` 统一为字符串。
- 不在 v1 中承载敏感密钥（accessKey/secretKey/token 等），避免误把系统设置当作密钥托管系统。
- 不包含前端 UI 的实现与交互接入（本提案仅定义 Server 侧能力与存储协议）。
- 不引入多租户隔离（multi-tenant）。

## What Changes

### 1) 新增：系统设置存储对象（etcd）

采用 Kubernetes 资源作为持久化载体（最终落盘在 etcd），建议以单例 ConfigMap 作为 v1 的存储形式：

- Namespace：`disaster-system`（可通过 Server 配置覆盖）
- Name：`disaster-platform-settings`
- `data.settings`：JSON 文本，保存 settings items

示例：

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: disaster-platform-settings
  namespace: disaster-system
data:
  settings: |
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

- 这里的 “存入 etcd” 指通过 Kubernetes API 写入资源（ConfigMap），由 Kubernetes 将其持久化到 etcd；不建议 Server 直接连 etcd。
- `items` 使用 `config_key -> item` 的映射，天然保证 key 唯一；`config_key` 字段允许冗余保存，便于调试与迁移。

### 2) 新增：媒体资源存储（Base64）

Server 负责将媒体文件编码为 Base64 并直接写入对应配置项的 `value`。

建议编码格式：

- `value` 使用 data URL：`data:<mime>;base64,<payload>`
- 支持 MIME：`image/png`、`image/jpeg`（SVG 是否支持可后续单独决策）

建议大小限制（必须在实现中固化）：

- 原始文件上限：`<= 256KB`
- 编码后字符串上限：`<= 350KB`

说明：Base64 约有 33% 体积膨胀，需要限制单个配置项大小，避免占满 ConfigMap 与 etcd。

### 3) 新增：Server API（CRUD + asset）

新增系统设置管理接口（路径与鉴权策略在 v1 固化）：

- `GET /api/v1/system-settings`（System Admin）
  - 列表返回所有配置项（`name/config_key/value/remark`）。
  - 列表返回必须按 `config_key` 升序排序（保证稳定性）。
- `GET /api/v1/system-settings/public?keys=...`（可选：公开）
  - 返回指定 keys 的配置项，用于未登录页面渲染（例如平台名称/Logo）。
  - 返回列表必须按 `config_key` 升序排序（保证稳定性）。
- `POST /api/v1/system-settings`（System Admin）
  - 创建配置项（`config_key` 必须唯一）。
- `PUT /api/v1/system-settings/{config_key}`（System Admin）
  - 更新配置项（name/value/remark）。
- `DELETE /api/v1/system-settings/{config_key}`（System Admin）
  - 删除配置项。
- `POST /api/v1/system-settings/assets/{config_key}`（System Admin）
  - 上传媒体文件，Server 编码为 Base64（data URL）并写入该配置项 `value`（若配置项不存在，可选择自动创建）。
- `GET /api/v1/system-settings/assets/{config_key}`（可选：公开）
  - 读取/下载该配置项对应的媒体资源（Server 从 Base64 解码后返回文件流）。

### 4) 权限与审计

- 写接口必须要求系统管理员身份（沿用现有 Server 认证体系）。
- 更新行为需记录 `updatedAt` 与 `updatedBy` 字段（用于审计与问题追踪）。

## Impact

- Product Impact
  - 平台级配置可运行时维护，无需发版即可调整名称/文案/链接/开关等。
  - 配置项的“展示名称/备注”可配置，便于运营与交付。
- Engineering Impact
  - Server 需实现：ConfigMap 读写（etcd 持久化）、CRUD API、Base64 编码/解码、上传与读取接口、鉴权与审计。
  - 本阶段不新增 RBAC 资源，默认沿用现有 Server ServiceAccount 权限。
- Compatibility
  - 对现有业务 API 无破坏性影响；新接口为增量能力。

## Risks

- 风险：缺少强类型 schema，可能出现 value 格式不一致。
  - 缓解：v1 通过命名约束（`config_key` 格式、长度限制）+ UI/使用方约定；未来可引入可选 schema 扩展提案。
- 风险：Base64 体积膨胀导致 ConfigMap/etcd 压力增加。
  - 缓解：限制原始文件大小与编码后字符串长度；超限直接返回 400 并拒绝写入。
- 风险：Base64 格式错误导致读取失败。
  - 缓解：上传路径统一由 Server 编码；对手工写入的 value 做解码校验，失败返回明确错误。
- 风险：并发更新导致覆盖。
  - 缓解：使用 Kubernetes `resourceVersion` 做乐观并发控制，冲突重试或返回 409。

## Open Questions

- `GET /api/v1/system-settings/public` 与 `GET /api/v1/system-settings/assets/{config_key}` 是否需要匿名访问？哪些 keys 允许公开？
- 是否需要支持 SVG（涉及安全与渲染兼容）？
- 是否需要提供“历史版本/回滚”能力，还是依赖审计日志与备份？
