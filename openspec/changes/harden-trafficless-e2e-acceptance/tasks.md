## 1. 提案与治理

- [x] 1.1 创建 proposal、design、tasks 和 `e2e-acceptance` 规范增量。
- [x] 1.2 创建并登记 ExecPlan 与决策日志。
- [x] 1.3 明确凭据脱敏/轮换、产品代码修改和旧证据覆盖均不在范围内。
- [x] 1.4 运行 `openspec validate harden-trafficless-e2e-acceptance --strict`。
- [x] 1.5 评审并批准本提案后进入实施阶段。

## 2. 旧执行重新定性

- [x] 2.1 在旧执行目录增加 review disposition，将其标记为探索性执行。
- [x] 2.2 修正 `02` 的事后补录说明，保留原始时间和内容。
- [x] 2.3 将统计修正为原始执行、修复回归和累计结果三个口径。
- [x] 2.4 拆分 `TC-BND-001` 的边界符合性 verdict 与 DEF-001 探索发现。
- [x] 2.5 补齐旧执行 `09-server-operator-mapping.md` 中可从现有证据确认的映射，未知项明确标记未验证。
- [x] 2.6 将旧执行功能结论修正为“核心 happy path + DEF-001 回归通过，未完成规范级功能验收”。

## 3. 被测版本冻结

- [x] 3.1 创建只包含四个关联变更的干净 acceptance branch/worktree。
- [x] 3.2 记录 Operator、Server、Web commit/tree 和干净工作树状态。
- [x] 3.3 构建固定 Operator 二进制或镜像，记录 SHA256/digest 和构建命令。
- [x] 3.4 从固定制品启动 Operator，记录启动参数、PID 和日志路径。
- [x] 3.5 禁止在同一 Run No 中热修源码；变更后使用新 commit/digest 和 Run No。

## 4. 执行前测试冻结

- [x] 4.1 创建新的正式 acceptance run 目录和标准报告骨架。
- [x] 4.2 在业务操作前完成 `01`、`02` 和 OpenSpec 场景追踪矩阵。
- [x] 4.3 生成测试计划 SHA256、批准时间和 reviewer 记录。
- [x] 4.4 生成每用例独立资源的 `instance-plan.tsv`。
- [x] 4.5 使用 `HARNESS_INSTANCE_PLAN_TSV` 运行命名空间隔离门禁。

## 5. F1 全量 E2E

- [x] 5.1 无 PVC 时成功 Skipped，且无 DataSync 下游备份/恢复对象。
- [x] 5.2 有 PVC 时继续完整 AppBackup/AppRestore/PVR 数据链路。
- [x] 5.3 labelSelector 只匹配 Pod 时通过 Pod 引用发现 PVC。
- [x] 5.4 无 PVC且 StorageRepository 不可用时仍成功 Skipped。
- [x] 5.5 有 PVC且 StorageRepository 不可用时保持失败语义。
- [x] 5.6 源集群 Pod list 失败时 DataSync 失败，不伪装成 Skipped。
- [x] 5.7 源集群 PVC list 失败时 DataSync 失败，不伪装成 Skipped。
- [x] 5.8 Skipped history 在 BackupRestoreStatistics 中计入 completed、不计入 failed。

## 6. F2 与 Drill 调度清理 E2E

- [x] 6.1 DataSync 组合用例验证 nodeName、nodeSelector、affinity 清理及数据恢复。
- [x] 6.2 Drill 组合用例验证调度清理、目标 registry 和 pull secret 共存。
- [x] 6.3 运行态验证 ResourceSync 和 shared builder 不继承 DataSync/Drill 专用规则。
- [x] 6.4 运行态验证 Drill resource restore 仍只有 scale-to-zero 规则。
- [x] 6.5 执行实际 Failover/ScaleUpTarget，保存目标业务 workload 扩容和就绪证据。
- [x] 6.6 对比 Failover 前后的 Deployment/StatefulSet `affinity`、`nodeSelector`、`topologySpreadConstraints`，证明业务模板字段未被清理。
- [x] 6.7 澄清 F2 与 Drill 两份活跃 spec 的“专用规则不继承”边界并严格验证。

## 7. F3 全量 E2E

- [x] 7.1 DataSync 使用当前恢复目标集群 registry，源目标 registry 不同时不得选源端。
- [x] 7.2 完成 failover/reprotect 角色反转后触发反向 DataSync，验证使用当前 secondaryCluster registry。
- [x] 7.3 显式 Trafficless 镜像优先于目标 registry。
- [x] 7.4 历史 `busybox:latest` 按目标 registry 解析为 `busybox:1.36`。
- [x] 7.5 历史 `busybox:1.36` 按目标 registry 解析。
- [x] 7.6 无目标 registry 时回退 `busybox:1.36`。
- [x] 7.7 Drill 使用演练目标集群 registry。
- [x] 7.8 Trafficless Pod 注入与目标 namespace Secret 名称一致的 imagePullSecrets。
- [x] 7.9 配置冲突的 `imageSources`/业务镜像前缀替换，运行态验证 Trafficless busybox 仍来自平台 registry。
- [x] 7.10 DataSync 恢复 namespace 同步 pull secret，并比较管理面与目标 `.dockerconfigjson` SHA256。
- [x] 7.11 Drill namespaceMapping 后只向实际目标 namespace 同步 pull secret，比较 SHA256，并证明源 namespace 无该 Secret。
- [x] 7.12 无 registry credential 时不创建 Secret、不注入 imagePullSecrets。
- [x] 7.13 registry credential Secret 不存在时在 AppRestore 创建前阻断并给出明确状态。
- [x] 7.14 registry credential Secret 类型错误时在 AppRestore 创建前阻断并给出明确状态。

## 8. Browser 与 Server 链路

- [x] 8.1 使用 Playwright 登录并从实例页面创建主路径 DisasterInstance。
- [x] 8.2 从演练页面创建 Drill 并通过 UI confirm。
- [x] 8.3 保存浏览器截图、请求 payload、响应状态和网络记录。
- [x] 8.4 每个 API/Browser 提交保存 `X-Trace-Id`。
- [x] 8.5 使用 Trace ID 关联 Server handler/service 运行日志；固定 access logger 不打印 Trace 字段，已按响应时间、method/path/status 和唯一资源关联，并显式记录该可观测性缺口。
- [x] 8.6 关联 Operator/CR、AppRestore、Kubernetes、Velero 和 MinIO 证据。
- [x] 8.7 补齐 `09-server-operator-mapping.md`，不得保留“待环境发现”。

## 9. 报告与发布判定

- [x] 9.1 `03` 按 Run No 保存原始 Passed/Failed/Blocked/Skipped。
- [x] 9.2 `04` 保存产品缺陷、环境事件、可观测性限制和发布门禁状态。
- [x] 9.3 `05` 从 `03` 重新计算探索、正式首轮、环境重试、全部 Run records 和当前唯一 Case 统计，不隐藏历史 Failed/Blocked。
- [x] 9.4 `06` 分别报告 Browser、API、Server、Operator/K8s/Velero/MinIO verdict。
- [x] 9.5 根据规范覆盖、缺陷和门禁给出 No-Go；功能 19/19 Passed，但候选新增 7 项 lint。

## 10. 门禁与收尾

- [x] 10.1 修复一份 2026-07-01 plan 的 2 个错误和两份 decision 的 8 个错误，共 10 个 Harness 结构错误。
- [x] 10.2 运行四个关联 OpenSpec 和本验收 change 的 `validate --strict`，全部通过。
- [x] 10.3 运行 `make harness-preflight`、`make harness-lint`、`make harness-ci`；均 0 fail，preflight 保留 3 warning。
- [x] 10.4 运行 `make test` 和 `make lint`，完整记录退出码、262 项候选结果和 255 项父提交基线。
- [x] 10.5 根据真实命令结果更新 `fix-datasync-initial-restore-safety/tasks.md` 的 4.2-4.5。
- [x] 10.6 精确清理本轮 `tfa-*` 资源及对象存储前缀，恢复/确认共享 Cluster、BSL、registry 和 imageSources 状态。
- [x] 10.7 更新 ExecPlan、决策日志、PLANS 和最终 No-Go 交付结论。
