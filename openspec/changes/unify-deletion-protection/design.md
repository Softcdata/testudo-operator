# Design: 通用依赖标签驱动的删除检查

## 1. 目标

本设计解决两个核心问题：

- 如何用统一模型表达跨模块依赖关系，避免每个模块各写一套依赖查询。
- 如何让 Server 提供统一检查结果 `upstream/downstream/can_delete`，供前端删除前决策。

设计原则：

- 统一标签协议。
- 旧标签不改。
- 创建即写入、变更即同步。
- 检查接口给出建议，删除接口保持现状。

## 2. 术语与方向

定义有向边 `A -> B`：表示资源 `A` 依赖资源 `B`。

- 对 `A` 来说，`B` 是 `downstream`。
- 对 `B` 来说，`A` 是 `upstream`。

建议判定：

- `can_delete = (len(upstream) == 0)`

说明：

- `can_delete` 是检查接口输出的建议值。
- 是否继续调用删除接口由前端决策。

## 3. 通用依赖标签协议

### 3.1 标签键

统一新增以下标签族：

- `testudo.softcdata.com/dependency-token=<token>`
- `testudo.softcdata.com/dependency-to-<token>=<relation-code>`

约束：

- `dependency-token` 每个资源仅 1 个。
- `dependency-to-*` 可有多条，每条表示一个下游目标。
- `<relation-code>` 必须是短码（<= 63 chars），如 `spec.config`、`spec.storageRepository`。

### 3.2 token 生成

`token` 由资源 UID 稳定派生：

- 输入：`metadata.uid`
- 算法：`sha256(uid)` 取前 16 位小写十六进制
- 示例：`9f2a31c4d77b1e90`

理由：

- 便于放入 label key（长度可控）。
- 不暴露完整 UID。
- 对同一资源稳定不变。

### 3.3 动态 key 规范

依赖边 key 模式：

- `testudo.softcdata.com/dependency-to-<target-token>`

查询上游时，给定目标资源 `T`：

1. 读取 `T` 的 `dependency-token`。
2. 生成 selector key：`testudo.softcdata.com/dependency-to-<T.token>`。
3. 在受支持资源类型中按该 key 进行统一查询。

## 4. 写入与同步策略

### 4.1 写入触发点

- 创建：资源创建成功后写入 `dependency-token` 与 `dependency-to-*`。
- 更新：依赖字段变更后重建 `dependency-to-*`。
- 删除：不新增边；由对象删除自然回收标签。

### 4.2 覆盖式重建

每次同步采用“先清后写”：

1. 删除当前资源所有 `dependency-to-*` 标签。
2. 根据当前依赖规则计算下游集合。
3. 回写新的 `dependency-to-*`。

这样可避免脏边残留。

### 4.3 存量回填

引入一次性回填流程：

1. 扫描 v1 覆盖模块所有存量资源。
2. 为缺失 `dependency-token` 的资源补 token。
3. 按规则重建 `dependency-to-*`。

实现约定（operator）：

- 在进程启动阶段提供一次性回填任务，默认开启，可通过启动参数 `--dependency-backfill-on-start=false` 关闭。
- 回填只更新 `dependency-token` 与 `dependency-to-*`，不改动既有业务标签。
- 回填失败按资源维度记录日志并继续处理其余对象，整体可幂等重跑。

## 5. 查询接口设计

### 5.1 Route

- Method: `POST`
- Path: `/api/v1/deletion/check`

### 5.2 Request

```json
{
  "resource_kind": "DisasterConfig",
  "name": "cfg-prod",
  "namespace": "disaster-system"
}
```

### 5.3 Response

```json
{
  "target": {
    "kind": "DisasterConfig",
    "name": "cfg-prod",
    "namespace": "disaster-system",
    "uid": "2f5c1c3e-8e4a-4f5d-8e4d-c4b8f93e4c91"
  },
  "upstream": [
    {
      "kind": "DisasterInstance",
      "name": "ins-a",
      "namespace": "disaster-system",
      "relation_code": "spec.config"
    }
  ],
  "downstream": [
    {
      "kind": "StorageRepository",
      "name": "repo-default",
      "namespace": "disaster-system",
      "relation_code": "spec.storageRepository"
    }
  ],
  "can_delete": false,
  "message": "has upstream references"
}
```

### 5.4 错误码

- `404`: 目标资源不存在。
- `500`: 查询失败或依赖解析失败。

说明：

- 检查接口不负责执行删除，也不返回删除阻塞专用错误码。

## 6. 查询算法

### 6.1 查 upstream

输入目标资源 `T`：

1. 读 `T.labels[dependency-token]` 得到 `tt`。
2. 构造 key `dependency-to-tt`。
3. 对注册资源类型逐类 `List`（带 label selector key exists）。
4. 将命中对象转换为 `upstream` 列表。

### 6.2 查 downstream

输入目标资源 `T`：

1. 扫描 `T.labels` 中 `dependency-to-*`。
2. 提取 token 与 `relation-code`。
3. 按 token 反查目标资源（优先 token 索引，失败时标记 unresolved）。
4. 输出 `downstream` 列表。

### 6.3 token 反查索引

为避免全量扫描，服务内部维护 token -> resource 的缓存索引（按 informer 增量更新）。

## 7. 删除执行策略（本阶段）

本阶段不将检查结果接入后端删除门禁：

1. 前端先调用 `POST /api/v1/deletion/check`。
2. 前端基于 `can_delete/upstream/downstream` 决定是否提示、是否继续删除。
3. 若前端继续调用既有 `DELETE`，后端保持当前行为与协议。

## 8. 规则来源与模块覆盖

规则来源固定为 `docs/platform-resource-dependency-audit.md` 的**模块真实调用依赖矩阵**。

约束：

- 依赖边定义、阻塞口径、状态过滤条件均从该矩阵提取。
- 矩阵到实现的逐条映射以 `openspec/changes/unify-deletion-protection/dependency-label-mapping.md` 为准。
- 若代码实现与矩阵冲突，应先修订矩阵并记录变更理由，再修改实现。
- 设计文档中的规则描述不得超出矩阵已确认范围。

v1 目标模块：

- Cluster
- StorageRepository
- DisasterPolicy
- DisasterConfig
- DisasterInstance
- DisasterGroup
- AppBackup
- AppRestore
- DisasterDrill
- DisasterBackup

内部来源模块：

- DisasterOperation
- DataSync
- ResourceSync
- DisasterJob（仅 Policy 兼容规则）

## 9. 一致性与兜底

为避免迁移期漏判，增加兜底规则：

- 若目标资源缺失 `dependency-token`，检查接口触发即时补写后重试一次。
- 若 token 反查失败，返回 `downstream` 中 `unresolved=true` 项，并记录告警。
- 在回填完成前，保留旧依赖判断作为只读校验与排障参考。

## 10. 测试设计

- 单元测试：
  - token 生成稳定性
  - `dependency-to-*` 覆盖式重建
  - `can_delete` 推导逻辑（由 `upstream` 派生）
- 集成测试：
  - 创建后标签写入
  - 更新后边重建
  - `/api/v1/deletion/check` 上下游结果正确
- 回归测试：
  - 既有业务标签不被修改
  - 既有删除接口调用方式与行为保持兼容

## 11. 非目标

- 不将依赖关系持久化到独立关系表。
- 不引入跨系统中心化图数据库。
- 不在本轮重定义 Finalizer 生命周期语义。
- 不在本轮把检查逻辑强制接入后端删除门禁。
