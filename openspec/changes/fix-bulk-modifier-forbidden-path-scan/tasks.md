## 1. Server 扫描修复
- [x] 1.1 在 `disaster-server` bulk snapshot 生成模块新增或复用禁止路径判断 helper。
- [x] 1.2 修改 `collectReplaceExactValueMatches`，遍历时跳过 `/status/**`、`/metadata/finalizers/**`、`/metadata/ownerReferences/**`。
- [x] 1.3 修改 `collectRemoveKeyMatches`，使用同一套禁止路径过滤。
- [x] 1.4 确认禁止路径过滤不会默认排除 `pods` 资源，也不依赖 `resourceSelection.excludedResources=["pods"]`。

## 2. Server 验证
- [x] 2.1 新增单测：`replaceExactValue` 同时命中 Deployment spec image 与 Pod status image 时，只生成 spec image 规则。
- [x] 2.2 新增单测：`replaceExactValue` 只命中 forbidden path 时，不生成 forbidden rule，并按零可执行命中失败。
- [x] 2.3 新增单测：`removeKey` 命中 `/status/**`、`/metadata/finalizers/**`、`/metadata/ownerReferences/**` 时跳过。
- [x] 2.4 回归单测：普通可修改路径的 `replaceExactValue` 和 `removeKey` 行为不变。
- [x] 2.5 API create/update 回归：包含运行中 Pod status 同值镜像时，不再报 `patch path /status/containerStatuses/0/image is forbidden`。

## 3. Operator / Contract
- [x] 3.1 确认 operator 侧 `validatePatchGovernance` 仍拒绝 forbidden path，不放松治理。
- [x] 3.2 确认 `resourceSelection` 语义不变，不把排除 pods 作为修复要求写入文档或默认值。

## 4. 校验
- [x] 4.1 运行 `openspec validate fix-bulk-modifier-forbidden-path-scan --strict`。
- [x] 4.2 在 `disaster-server` 运行相关单测。
- [x] 4.3 若涉及 API 文档，按 server API 文档同步流程更新 Swagger / Apipost。
