## Context

现有执行目录混合了测试设计、探索执行、缺陷修复和修复后回归。Operator 由脏工作树通过 `go run` 启动，报告没有版本制品身份；测试设计文件在执行结束后补写；Browser 实际停在登录页；Server 映射仍有“待环境发现”；F2/F3 复用资源；Harness 门禁存在历史结构错误。这些问题不会推翻已观察到的主路径事实，但会阻止结果成为可交付版本的正式验收证明。

本变更建立一次新的正式 acceptance run。它治理测试和证据，不改变产品行为。

## Current Functional Coverage Baseline

- 已完整观察：F1 无 PVC成功跳过；F2 nodeName/nodeSelector/affinity 清理主路径；F3 DataSync/Drill 私有仓库 happy path；DEF-001 Drill 调度清理回归。
- 部分观察：F1 有 PVC继续同步；F2 ResourceSync 不受污染；F2 业务 workload 模板未修改；F3 pull secret 注入和内容来源。
- 尚未执行：F1 labelSelector、StorageRepository 双分支、Pod/PVC list 双故障、Skipped 统计；F2 实际 Failover 和业务调度字段前后对比；F3 角色反转、显式镜像、历史默认值、无 registry、无凭据、错误凭据、业务镜像运行态隔离；Browser 用户入口。
- 因此旧执行的准确功能结论是“核心 happy path 和 DEF-001 回归通过”，不是“完整功能 E2E 验收通过”。

## Goals / Non-Goals

### Goals

- 使每条正式验收结论绑定到唯一源码版本和构建制品。
- 在执行前冻结测试预期，避免事后拟合。
- 对四个关联 OpenSpec 的运行时强制场景形成完整、可审计的覆盖矩阵。
- 区分 Browser、API、Server、Operator、Kubernetes、Velero 和 MinIO 各层 verdict。
- 保证用例隔离、历史结果保真、统计可重算。
- 让 release recommendation 直接由缺陷和门禁状态推导。

### Non-Goals

- 不修改产品实现来迎合测试。
- 不在同一个 acceptance run 中热修代码后继续累计结果。
- 不把单元测试或静态代码摘录冒充规范要求的运行态 E2E。
- 不处理凭据脱敏、轮换、撤销或证据目录安全分发。

## Decisions

### Decision 1: 旧执行只作为探索性证据

保留旧目录全部原始证据，并增加 disposition 说明。旧执行可以证明已观察事实和 DEF-001 的发现过程，但不得继续被包装为冻结版本的正式验收。

### Decision 2: 版本身份使用源码和制品双重冻结

正式执行必须从独立干净 worktree 的 acceptance commit 构建。运行清单同时记录 Git commit/tree、`git status --porcelain`、构建命令，以及 Operator 二进制 SHA256 或镜像 digest。禁止从主脏工作树执行 `go run`。

若测试过程中修改代码，当前 run 立即停止；新代码必须形成新 commit/digest，并使用新的 Run No 和证据目录重新执行受影响范围。

### Decision 3: 测试定义在业务操作前冻结

`02-test-cases.md` 在创建任何测试业务资源前生成 SHA256，并记录批准时间。文件只写前置条件、步骤和期望；实际结果只写 `03-execution-results.md` 和 `04-defects.md`。改变 Expected 时创建新版本并重新批准，不覆盖旧文件。

### Decision 4: 规范场景逐项映射

建立 `OpenSpec change -> requirement -> scenario -> Case ID -> evidence -> verdict` 矩阵：

- F1 覆盖 7 个场景。
- F3 覆盖 13 个场景。
- F2 DataSync 调度清理和 Drill 专用调度清理覆盖节点约束、目标 registry 共存以及 ResourceSync/shared builder/Failover/业务模板边界。

同一运行可以覆盖多个场景，但必须明确标记为一个组合 Case ID，不能把一次执行重复计为多个独立 Passed。

一个规范场景若包含多个可独立失败的代码入口，必须拆成独立 Case ID。F1 的源集群发现失败分别测试 Pod list 和 PVC list；F3 的凭据同步失败分别测试 Secret 不存在和 Secret 类型错误。

### Decision 5: 独立用例必须使用独立资源

执行前生成 `instance-plan.tsv`，包含 Case ID、源/目标 namespace、Instance、DataSync、ResourceSync、Drill 和 marker。通过 `HARNESS_INSTANCE_PLAN_TSV` 门禁检查冲突。每个独立 Case ID 必须使用不同 namespace 和资源名。

### Decision 6: Browser 与 API verdict 分开

实例创建、Drill 创建和 confirm 等已有 Web 入口使用 Playwright 登录后执行。环境故障注入或 Web 未暴露的配置可使用 API/Kubernetes，但报告必须标记为 `API/Operator E2E`。缺少认证时 Browser 为 `Blocked(Auth)`，不会被 API 成功抵消。

### Decision 7: Server 运行链路必须由 Trace 证据闭环

每个 API/Browser 提交保存请求、响应、HTTP 状态和 `X-Trace-Id`；再保存同一 Trace 的 Server handler/service 日志、Operator 日志或 CR annotation、下游 AppRestore/Velero 对象。若 trace 未传播到某层，必须明确记录观测缺口，不能以静态代码映射替代运行日志。

### Decision 8: 历史执行和当前缺陷状态分别统计

`03` 保存每次原始 verdict；`04` 保存缺陷生命周期；`05` 同时展示各 Run No 原始统计和当前缺陷状态；`06` 根据所有层级和门禁给出 release recommendation。历史 Failed 永不改写为 Passed 或从计数中消失。

### Decision 9: 门禁决定发布建议

- `Go`: 所有强制 E2E、Browser 主路径、OpenSpec、Harness、`make test` 和 `make lint` 均通过，无未解决 P0/P1。
- `Conditional Go`: 产品 E2E 通过，但存在明确、获批且与本变更无关的门禁债务，例如仓库既有 lint 基线。
- `No-Go`: 任一强制场景失败或缺失、Browser 主路径被要求但 Blocked、版本身份不明、测试定义未冻结、Harness 失败，或存在未解决 P0/P1。

### Decision 10: 凭据治理明确留在范围外

本提案遵循用户决定，不执行脱敏或轮换。该决定只缩小实施范围，不改变现有证据目录的安全属性；正式 acceptance run 的共享与归档安全性不由本提案证明。

### Decision 11: 外溢和 Secret 来源使用运行态差异证明

F2 Failover 边界不能只依赖代码审计。正式用例必须在 Failover 前后分别导出 Deployment 和 StatefulSet 的 `spec.template.spec.affinity`、`nodeSelector`、`topologySpreadConstraints`，证明 ScaleUpTarget 未清理业务调度字段。

F3 pull secret 来源不能只证明 Secret 存在。正式用例必须分别计算管理面引用 Secret 和目标 namespace Secret 中 `.dockerconfigjson` 原始字节的 SHA256，并证明 hash 一致；Drill namespaceMapping 场景还必须证明 Secret 未错误同步到源 namespace。业务 `imageSources`/镜像前缀替换隔离必须通过实际冲突配置和最终 Trafficless 镜像验证，不以静态代码摘录代替。

## Artifacts

正式执行至少产生：

- `00-build-manifest.txt`
- `01-feature-inventory.md`
- `02-test-cases.md`、`02-test-cases.md.sha256`、`02-approval.txt`
- `03-execution-results.md`
- `04-defects.md`
- `05-traceability-matrix.md`
- `06-final-report.md`
- `07-case-trace-cards.md`
- `08-remote-verification-log.md`
- `09-server-operator-mapping.md`
- `instance-plan.tsv`
- `openspec-scenario-case-matrix.tsv`
- 每个 Case ID 独立的请求、响应、日志、CR、Kubernetes、Velero 和 MinIO 证据目录

## Risks / Trade-offs

- 完整覆盖会显著增加执行时间，但这是把“主路径已观察”升级为“规范全部验收”的必要成本。
- 角色反转、StorageRepository 故障和错误凭据场景会改变共享环境状态；必须使用可回滚配置、执行前快照和串行恢复。
- Browser 认证依赖外部账号状态；缺少认证时只能给 Blocked/No-Go，不能降级为 API Passed。
- 仓库既有 `make lint` 债务可能阻止无条件 Go；若不单独清理，只能给 Conditional Go。
- 凭据治理不在范围内，因此本提案不能解除旧证据目录的分发风险。

## Migration Plan

1. 将旧执行报告改标为探索性执行并修正统计/映射，不改变原始证据。
2. 创建干净 acceptance worktree、commit 和制品。
3. 冻结并批准测试设计及 namespace 计划。
4. 在 170/171 环境执行 Browser 和 API/Operator E2E。
5. 新缺陷修复后创建新 commit/digest 和新 Run No，再做定向回归。
6. 修正报告、完成门禁并给出正式发布建议。

## Open Questions

- Playwright 正式执行使用哪个测试账号和认证方式，需要在执行前写入环境前置条件，但凭据本身不得写入提案。
- `make lint` 的 261 项既有债务是先清理，还是由负责人书面批准为 Conditional Go 条件，需要在正式执行前决定。
