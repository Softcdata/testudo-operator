# Tasks

## 1. Proposal
- [ ] 1.1 评审并确认首期执行路径固定为 kind 级白名单
- [ ] 1.2 评审并确认对象级精确恢复明确留作后续 proposal

## 2. Operator
- [ ] 2.1 为 `RestoreResourceSelectionPolicy` 补齐 scoped 四字段
- [ ] 2.2 在 restore policy 应用链路中补齐 scoped 四字段的翻译逻辑
- [ ] 2.3 在 DataSync / ResourceSync 备份构造链路中对齐 scoped 四字段
- [ ] 2.4 当 `includeClusterResources=true` 时保持旧优先级，忽略 scoped 四字段
- [ ] 2.5 为 kind 级 cluster-scoped 选择与降级语义补 controller tests

## 3. Server / Web Alignment
- [ ] 3.1 复核 server 已有 scoped API 与 operator 新 CRD 字段名完全一致
- [ ] 3.2 在 web 文案中明确“cluster-scoped 只支持 kind 级范围，不支持对象级精确恢复”

## 4. Verification
- [ ] 4.1 `openspec validate update-scoped-resource-execution-model --strict`
- [ ] 4.2 以 `StorageClass`、`IngressClass`、`PersistentVolume` 三类资源验证首期语义与降级提示
