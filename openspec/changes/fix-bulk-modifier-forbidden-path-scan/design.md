# 设计：bulk modifier 扫描阶段过滤禁止路径

## 1. 背景
`bulkModifierActions` 采用“用户动作 + server 生成快照 + operator 消费快照”的模型。server 会连接源集群，扫描实例保护范围内资源，并将批量动作展开为 `modifierRuleSnapshot`。

当前 `replaceExactValue` 的扫描是通用 JSON 树遍历：只要字符串叶子节点等于 `sourceValue` 就生成 match。该模型会把 Kubernetes status 子树中的镜像值也作为候选修改点，例如：

```text
/status/containerStatuses/0/image
```

但资源修改器治理规则禁止修改 `/status/**`。因此 bulk 生成器产生了后续必然被拒绝的规则，导致整个实例保存失败。

## 2. 设计原则
1. 生成器只生成可执行规则，不生成必然被治理拒绝的规则。
2. 禁止路径治理是路径级语义，不是资源类型语义；不得通过默认排除 `pods` 修复。
3. `resourceSelection` 不承担安全治理职责；它只表达用户希望扫描/恢复哪些资源。
4. 最终规则校验继续保留，作为手写规则和异常快照的兜底防线。

## 3. 禁止路径清单
bulk 扫描阶段必须跳过：

```text
/status
/status/*
/metadata/finalizers
/metadata/finalizers/*
/metadata/ownerReferences
/metadata/ownerReferences/*
```

该清单应与 server 现有 `validateModifierPatchPath` 以及 operator 编译期 `validatePatchGovernance` 保持一致。

## 4. 实现方案

### 4.1 新增路径判断 helper
在 `disaster-server/internal/apis/disaster_instance/v1` 中新增或复用 helper：

```go
func bulkPatchPathForbidden(path string) bool
func bulkPatchPathAllowed(path string) bool
```

建议让 helper 覆盖完整禁止路径清单，并避免与 `validateModifierPatchPath` 语义漂移。

### 4.2 replaceExactValue 扫描剪枝
`collectReplaceExactValueMatches` 在遍历 map/list 子节点前先计算 child path。若 child path 是禁止路径或禁止路径子树，直接跳过该子树。

命中字符串叶子节点时，只有 `bulkPatchPathAllowed(path)` 才允许 append match。

### 4.3 removeKey 扫描剪枝
`collectRemoveKeyMatches` 同样在遍历 map/list 子节点前判断 child path。若 child path 是禁止路径或禁止路径子树，直接跳过。

当 `key == targetKey` 时，也必须确认对应 child path 允许修改后才能 append match。

### 4.4 零命中语义
现有 `matched zero resources` 语义保持不变：

- 如果过滤禁止路径后没有任何可执行 match，bulk action 继续失败。
- 后续可增强错误信息，例如区分 `matched only forbidden paths`，但本变更不强制要求。

## 5. 示例

### 输入
用户声明：

```json
{
  "id": "replace-bkcmdb-synchronizer-image",
  "action": "replaceExactValue",
  "enabled": true,
  "applyTo": ["resourceSync", "drill"],
  "sourceValue": "10.11.11.1:5000/blueking/bcs-bkcmdb-synchronizer:v1.30.0",
  "targetValue": "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/bcs-bkcmdb-synchronizer:v1.30.0",
  "directionPolicy": "Auto"
}
```

源集群中同时存在：

```text
Deployment /spec/template/spec/containers/0/image = sourceValue
Pod        /status/containerStatuses/0/image = sourceValue
```

### 输出
`modifierRuleSnapshot` 只包含 Deployment spec 规则，不包含 Pod status 规则。

## 6. 验证策略
1. `replaceExactValue` 同时命中 spec 和 status 时，只生成 spec 规则。
2. `replaceExactValue` 只命中 status 时，不生成 forbidden rule，并按零可执行命中失败。
3. `removeKey` 命中 `/status/**`、`/metadata/finalizers/**`、`/metadata/ownerReferences/**` 时跳过。
4. 原有可修改路径的 `replaceExactValue` / `removeKey` 行为不变。
5. API create/update 使用运行中 Pod status 包含同镜像值的实例时，不再返回 `/status/containerStatuses/0/image is forbidden`。
