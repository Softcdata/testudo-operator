# Tasks: scoped 资源过滤字段补齐

## 1. 类型与 CRD

- [x] 1.1 扩展 `RestoreResourceSelectionPolicy`，新增四个 scoped 字段
- [x] 1.2 生成并校验 CRD schema，确保字段出现在 `disasterinstances` CRD

## 2. 恢复侧优先级、映射与校验

- [x] 2.1 在 `applyResourceSelectionPolicy` 增加优先级判定（`includeClusterResources=true` 时忽略 scoped 四字段）
- [x] 2.2 实现 scoped -> RestoreSpec 映射逻辑（`includedResources` / `excludedResources` / `includeClusterResources`）
- [x] 2.3 实现 scoped include/exclude 冲突与通配符组合校验
- [x] 2.4 补充生效模式可观测信息（注解或日志）

## 3. 提交期拒绝（webhook）

- [x] 3.1 在 `DisasterInstance` validating webhook 中接入 `resourceSelection` 校验
- [x] 3.2 保证校验失败返回可识别错误信息（含字段路径与冲突原因）
- [x] 3.3 当 `includeClusterResources=true` 时，校验逻辑忽略 scoped 四字段冲突

## 4. 回归测试

- [x] 4.1 增加恢复策略单元测试：`includeClusterResources=true` 时忽略 scoped 四字段
- [x] 4.2 增加恢复策略单元测试：scoped 模式映射正确
- [x] 4.3 增加 webhook 单元测试：非法 scoped 组合在提交期被拒绝
- [x] 4.4 增加 AppBackup 侧回归测试：scoped 四字段可透传到 Velero Backup/Schedule

## 5. 验证与交付

- [x] 5.1 执行 `go test ./internal/controller/restore ./internal/webhook/disasterinstance ./internal/controller/appbackup`
- [x] 5.2 执行 `openspec validate add-scoped-resource-selection-filters --strict`

## 备注

- `go test ./internal/controller/appbackup` 包含既有用例失败（与本提案改动无关）；新增 scoped 透传定向用例通过。
