## 1. 提案与边界

- [x] 1.1 确认当前无 PVC DataSync 状态与 Drill 固定 FullRestore 缺陷链路。
- [x] 1.2 冻结 ResourceOnly 三重证据、普通备份缺失失败关闭和 `skipValidation` 安全边界。
- [x] 1.3 明确实例、同质组、混合组以及 Ready 到 Confirm 状态漂移语义。
- [x] 1.4 运行 `openspec validate support-resource-only-drill-without-pvc --strict`。

## 2. API 与生成物

- [x] 2.1 为 RestoreMode 增加 `ResourceOnly`，并增加仅供组聚合回显的 `Mixed`。
- [x] 2.2 在 DisasterDrill status 增加逐实例恢复模式快照。
- [x] 2.3 在 DisasterOperation DrillConfig 增加单实例模式和组模式 map。
- [x] 2.4 生成 deepcopy、client 与 CRD，并更新 Drill 读取 DataSync/ResourceSync 的 RBAC。

## 3. Drill 预检

- [x] 3.1 实现 ResourceSync 必备检查与 DataSync FullRestore/ResourceOnly/Invalid 分类。
- [x] 3.2 实例 Drill 在 Pending 阶段写真实 BackupAvailable、恢复模式和模式快照。
- [x] 3.3 组 Drill 对所有成员分类并写 FullRestore/ResourceOnly/Mixed 聚合状态。
- [x] 3.4 普通备份缺失在 Pending 失败，不创建 Operation；`skipValidation` 不得绕过。
- [x] 3.5 Confirm 前复核成员集合与模式快照，处理历史 Ready 对象和状态漂移。

## 4. Operation 编排

- [x] 4.1 DisasterDrill 创建 Operation 时传递冻结后的模式配置。
- [x] 4.2 ResourceOnly Operation 只初始化 RestoreResource 与 ScaleUp。
- [x] 4.3 FullRestore 与历史空模式保持 RestoreResource、RestoreData、ScaleUp。
- [x] 4.4 Group 创建子 Operation 时 deep-copy DrillConfig，并逐实例收敛 restoreMode。

## 5. 测试

- [x] 5.1 分类表覆盖完整 no-data、错误/缺失 condition、非 Skipped history、非 Ready 和陈旧 backup name。
- [x] 5.2 Pending 预检覆盖 ResourceOnly、FullRestore、DataSync 缺备份、ResourceSync 缺备份和 skipValidation。
- [x] 5.3 Confirm 覆盖模式透传、历史 Ready 快照补齐和状态漂移阻断。
- [x] 5.4 Operation 覆盖 ResourceOnly 两步骤、FullRestore 三步骤以及不创建 data AppRestore。
- [x] 5.5 Group 覆盖 Mixed 聚合、逐实例模式与 DrillConfig 独立副本。

## 6. 验证与审核

- [x] 6.1 运行 DisasterDrill/DisasterOperation 定向测试及 DataSync/ResourceSync/restore 边界回归。
- [x] 6.2 运行 `make test` 与 `make lint`，准确记录既有失败。
- [x] 6.3 运行 `make harness-preflight`、`make harness-lint`、`make harness-ci`。
- [x] 6.4 再次运行 OpenSpec strict 校验并审核 diff 仅影响 Drill 行为。
- [x] 6.5 更新 ExecPlan、决策日志和本任务清单。
