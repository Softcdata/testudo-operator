# Change: 固化 Trafficless 三项改动的正式 E2E 验收

## Why

现有 `trafficless-3changes` 执行足以证明 F1 无 PVC 主路径、F2 DataSync 调度清理主路径、F3 DataSync/Drill 私有仓库 happy path 和 DEF-001 回归能够工作，但不足以完成三个功能变更的规范级验收。labelSelector、StorageRepository 两个分支、源集群 Pod/PVC 查询失败、Skipped 统计、角色反转、默认值兼容、无凭据/错误凭据、业务镜像隔离、Failover 外溢和 Browser 入口仍缺少运行态证据。该执行同时不能绑定到一个干净可复现的版本，测试定义晚于执行，Server 运行链路也未闭环。继续修改原报告无法把探索性结果转化为正式验收结果，需要建立一轮可冻结、可追踪、可复现的正式 acceptance run。

## What Changes

- 将现有 `trafficless-3changes-e2e-20260709-222335-exec-20260709-222335` 定位为探索性执行，保留原始成功、失败和回归证据，不再作为正式发布验收基线。
- 正式执行前冻结 Operator/Server/Web 的 Git commit、干净工作树状态以及 Operator 二进制 SHA256 或镜像 digest。
- 正式执行前冻结测试定义，记录测试计划 SHA256、批准时间和关联 OpenSpec 版本；执行结果只能写入结果与缺陷文件。
- 建立 OpenSpec 场景到 E2E Case ID 的完整追踪矩阵，补齐 F1 的 7 个强制场景、F3 的 13 个强制场景，以及 F2/Drill 调度清理和外溢边界运行态验证；同一场景包含多个独立故障入口时必须拆分执行，例如 Pod list/PVC list、凭据缺失/类型错误。
- 对 Failover 前后的 Deployment/StatefulSet 调度字段做字段级快照对比，并通过管理面/目标 namespace dockerconfigjson 的 SHA256 证明 pull secret 内容来源。
- 每个独立 Case ID 使用独立源/目标 namespace、实例、同步对象、演练对象和数据 marker；组合验证只能使用一个组合用例编号。
- 对已有 Web 入口的实例创建、Drill 创建和 confirm 使用 Playwright 执行；无法登录时将 Browser 子用例标记为 `Blocked(Auth)`，不得伪装为 Passed。
- 每个 API 用例保存 `X-Trace-Id` 并关联 Server handler/service 日志、Operator/CR、Kubernetes、Velero 和 MinIO 证据。
- 分别统计原始执行、缺陷复现和修复回归，保留历史 Failed；覆盖矩阵不得将历史失败改写为 0。
- 修复 Harness decision 文档结构债务，完成 OpenSpec、Harness、测试和 lint 门禁，并根据实际门禁给出 `Go`、`Conditional Go` 或 `No-Go`。

## Non-Goals

- 不修改 Operator、Server 或 Web 的产品行为、CRD/API 和运行时语义。
- 不在本提案中修复执行过程中发现的新产品缺陷；新缺陷按现有 Bug/OpenSpec 流程单独处理并重新冻结版本。
- 按用户明确边界，本提案不处理现有证据目录中的凭据脱敏、轮换或撤销，也不将该目录转换为可对外共享的安全制品。
- 不覆盖或删除现有探索性执行的原始证据。
- 不把当前已通过的主路径结果描述为完整功能规格覆盖。

## Impact

- Affected specs: 新增 `e2e-acceptance` 验收能力。
- Related changes: `add-datasync-no-pvc-skip`、`fix-datasync-initial-restore-safety`、`add-target-registry-trafficless-restore`、`fix-drill-trafficless-scheduling-cleanup`。
- Affected repositories: `disaster-operator` 的 OpenSpec/Harness 文档，`disaster-test` 的正式验收目录，以及 `disaster-server`/`cluster-disaster-web` 的只读版本与运行证据。
- Product code impact: 无；本提案阶段和验收实施阶段均不得直接修改 `internal/`、`pkg/`、`cmd/`、`config/`。
- Operational impact: 需要在 170/171 环境重新构建、部署并执行完整验收套件，运行期间创建带唯一测试前缀的临时资源。
