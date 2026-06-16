# 实施任务

## 1. Operator API 与生成物
- [x] 1.1 在 `DisasterInstanceSpec` 增加 `veleroHooks`，其中 `dataBackup` / `dataRestore` 必须使用指针字段以保留 presence/clear 语义。
- [x] 1.2 在 `DisasterDrillSpec` 与 `DisasterOperation.DrillConfig` 增加演练级 `veleroHooks`。
- [x] 1.3 扩展 `SyncHistoryRecord`，增加 `backupHookStatus` / `restoreHookStatus` 汇总字段。
- [x] 1.4 重新生成 deepcopy、CRD manifests、clientset/lister/informer。

## 2. Operator 投影逻辑
- [x] 2.1 确认 AppBackup -> Velero Backup 透传 `template.hooks`。
- [x] 2.2 确认 AppBackup -> Velero Schedule 透传 `template.hooks`。
- [x] 2.3 确认 AppRestore -> Velero Restore 透传 `template.hooks`。
- [x] 2.4 DataSync build AppBackupSpec 投影 `instance.spec.veleroHooks.dataBackup`。
- [x] 2.5 DataSync 对既有 `ds-*` AppBackup 执行 desired spec/template 对齐，至少检测并更新 `Template.Hooks` 差异，并测试“实例 hooks 更新后下一次备份使用新 hooks”。
- [x] 2.6 DataSync build AppRestoreSpec 投影 `instance.spec.veleroHooks.dataRestore`。
- [x] 2.7 DataSync 同步历史写入 `backupHookStatus` / `restoreHookStatus`。
- [x] 2.8 DisasterDrillReconciler 将 `drill.spec.veleroHooks` 复制到 `operation.spec.drillConfig.veleroHooks`。
- [x] 2.9 Drill data restore 使用 `operation.spec.drillConfig.veleroHooks.dataRestore` 覆盖实例级 dataRestore。
- [x] 2.10 ResourceSync 明确不投影 hooks，并以测试锁定。
- [x] 2.11 `restorePVs=true` 自动注入的 PVC `spec.volumeName` 清理规则必须幂等，不得因字段不存在导致 Velero Restore `PartiallyFailed`。
- [x] 2.12 AppRestore 必须将 Velero Restore `PartiallyFailed` 暴露为顶层 `PartiallyFailed`，DataSync、ResourceSync、Drill 必须按非成功终态处理。
- [x] 2.13 Drill 显式空 `veleroHooks:{}` 必须作为清空覆盖复制到 Operation，执行端不得继承实例级 dataRestore Hook。

## 3. Server API
- [x] 3.1 AppBackup create/update/detail/list DTO 支持 `hooks`。
- [x] 3.2 AppRestore create/update/detail/list DTO 支持 `hooks`。
- [x] 3.3 DisasterInstance create/update/detail/list DTO 支持 `veleroHooks`。
- [x] 3.4 DisasterDrill create/detail/list/watch DTO 支持 `veleroHooks.dataRestore`。
- [x] 3.5 实现 create/update/clear 字段 presence 语义；`veleroHooks.dataBackup` 和 `veleroHooks.dataRestore` 必须分别有 presence flag 或 RawMessage。
- [x] 3.6 实现基础 Hook 结构校验与错误响应。
- [x] 3.7 校验 command 数组保持透传，不做平台占位符渲染。
- [x] 3.8 对明显敏感参数明文传入进行硬拒绝，错误码为 `VeleroHookSensitiveParameter`，并返回字段路径。
- [x] 3.9 校验 timeout 上限：Backup exec `timeout<=10m`，Restore exec `execTimeout<=10m`，Restore exec `waitTimeout<=30m`，Restore init `timeout<=30m`。
- [x] 3.10 同步历史 DTO 回显 `backupHookStatus` / `restoreHookStatus`。
- [x] 3.11 AppRestore create/update 中 `restorePVs=true` 或 `cleanVolumes=true` 自动追加的 PVC `volumeName` 清理规则必须使用幂等 patch，并替换 legacy `remove /spec/volumeName`。
- [x] 3.12 DisasterDrill create 必须保留顶层 `veleroHooks:{}` 的 presence，写入非 nil 空对象以表达清空继承。

## 4. Server 文档同步
- [x] 4.1 更新 Swagger/OpenAPI schema、请求示例、响应示例。
- [x] 4.2 更新 RunAPI/Apipost 对应接口说明和示例。
- [x] 4.3 更新本地接口证据清单。
- [x] 4.4 文档示例必须覆盖 exec command argv 传参、restore initContainer env/envFrom 传参、敏感参数不明文传入。

## 5. Web
- [ ] 5.1 AppBackup 高级 Hook JSON 输入与回显。
- [ ] 5.2 AppRestore 高级 Hook JSON 输入与回显。
- [ ] 5.3 DisasterInstance 容灾 Hook 输入与回显。
- [ ] 5.4 DisasterDrill dataRestore Hook 覆盖输入与回显。
- [ ] 5.5 HookStatus 在备份/恢复/同步历史中展示；同步历史必须消费 server DTO 的 `backupHookStatus` / `restoreHookStatus` 字段。

## 6. 验证
- [x] 6.1 `make generate`
- [x] 6.2 `make manifests`
- [x] 6.3 `go test ./pkg/apis/disaster/v1 ./internal/controller/appbackup ./internal/controller/apprestore ./internal/controller/datasync ./internal/controller/resourcesync ./internal/controller/disasteroperation ./internal/controller/disasterdrill ./internal/controller/restore -count=1`
- [x] 6.4 server 相关 handler 单测。
- [ ] 6.5 web 类型检查与构建。
- [x] 6.6 `openspec validate add-velero-hook-passthrough --strict`
- [x] 6.7 回归测试覆盖 PVC 清理幂等、AppRestore `PartiallyFailed` 映射、DataSync/ResourceSync/Drill 非成功终态处理。
- [x] 6.8 回归测试覆盖 AppRestore 直建 PVC 清理幂等、legacy remove 替换、Drill `veleroHooks:{}` 清空继承。
