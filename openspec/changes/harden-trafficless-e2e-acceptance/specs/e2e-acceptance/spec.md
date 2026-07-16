## ADDED Requirements

### Requirement: 正式 E2E 验收必须绑定可复现版本

正式验收必须 (MUST) 绑定到唯一、可重建的源码版本和 Operator 构建制品，不得使用身份不明的脏工作树运行结果支持发布结论。

#### Scenario: 从干净版本构建并执行

- **GIVEN** 一轮正式 E2E 验收准备开始
- **WHEN** 启动被测 Operator
- **THEN** 验收记录必须包含 Operator、Server 和 Web 的 Git commit
- **AND** Operator 构建工作树必须干净
- **AND** 必须记录 Operator 二进制 SHA256 或镜像 digest
- **AND** 必须记录构建和启动命令

#### Scenario: 执行中发生代码修改

- **GIVEN** 一轮 E2E 已绑定到特定 commit 和制品
- **WHEN** 为修复缺陷修改了被测代码
- **THEN** 当前 Run No 不得继续累计修复后结果
- **AND** 必须生成新的 commit 和制品身份
- **AND** 必须使用新的 Run No 执行受影响回归

### Requirement: 测试定义必须在执行前冻结

正式验收的测试前置条件、步骤和期望结果必须 (MUST) 在创建测试业务资源前冻结，执行结果不得反向修改冻结预期。

#### Scenario: 冻结测试计划

- **GIVEN** 测试设计已经完成评审
- **WHEN** 开始第一条业务测试操作
- **THEN** 必须已经记录测试定义文件的 SHA256
- **AND** 必须记录批准时间、reviewer 和关联 OpenSpec 版本
- **AND** Expected 字段不得包含尚未执行得到的 Actual 结果

#### Scenario: 执行后需要修改预期

- **GIVEN** 测试执行已经开始
- **WHEN** 发现原 Expected 需要调整
- **THEN** 不得覆盖原冻结文件
- **AND** 必须创建新的测试定义版本并重新批准
- **AND** 原执行结果必须保留在原 Run No 下

### Requirement: OpenSpec 强制场景必须可追踪地验收

正式验收必须 (MUST) 将范围内每个 OpenSpec MUST 场景映射到 Case ID、执行层级、原始证据和 verdict，不得用少量 happy path 推导未执行场景通过。

#### Scenario: 建立规范覆盖矩阵

- **GIVEN** 一轮验收覆盖一个或多个 OpenSpec change
- **WHEN** 冻结测试设计
- **THEN** 每个 Requirement/Scenario 必须映射到至少一个 Case ID
- **AND** 每个 Case ID 必须声明 Browser、API、Server、Operator、Kubernetes、Velero 和 MinIO 中适用的证据层级
- **AND** 缺失运行态证据的强制场景不得标记 Passed

#### Scenario: 一个工作流组合验证多个场景

- **GIVEN** 一次业务执行可以同时验证多个规范场景
- **WHEN** 设计测试用例
- **THEN** 必须使用一个明确的组合 Case ID
- **AND** 覆盖矩阵可以将多个场景映射到该 Case ID
- **AND** 结果统计不得把该次执行重复计算为多个独立 Passed

#### Scenario: 一个规范场景包含多个独立故障入口

- **GIVEN** 一个 OpenSpec Scenario 使用“一个或多个外部调用失败”描述错误行为
- **AND** 不同外部调用由独立代码路径执行
- **WHEN** 设计正式 E2E
- **THEN** 每个可独立失败的调用路径必须具有独立 Case ID
- **AND** Pod list/PVC list 或 Secret 缺失/Secret 类型错误不得只执行其中一个后推导另一个通过

### Requirement: 独立 E2E 用例必须隔离资源

每个独立 Case ID 必须 (MUST) 使用独立的业务 namespace、实例、同步/演练对象和数据标记，避免共享状态造成归因混淆。

#### Scenario: 执行独立用例

- **GIVEN** 两个不同的 Case ID 将在同一 170/171 环境执行
- **WHEN** 创建测试资源
- **THEN** 两个用例不得复用源或目标 namespace
- **AND** 不得复用 DisasterInstance、DataSync、ResourceSync、Drill 或 marker
- **AND** 执行前必须通过 namespace 计划冲突检查

### Requirement: Browser 与 API 验收必须分别判定

已有 Web 用户入口的主路径必须 (MUST) 通过真实浏览器执行；API 成功不得替代未执行的 Browser verdict。

#### Scenario: Web 主路径可用

- **GIVEN** 实例创建或 Drill 创建/确认已有 Web 入口
- **AND** 测试环境具有有效认证条件
- **WHEN** 执行正式主路径验收
- **THEN** 必须使用 Playwright 完成用户操作
- **AND** 必须保存关键截图和浏览器网络请求/响应
- **AND** Browser 和 API verdict 必须分别记录

#### Scenario: 无法完成 Web 认证

- **GIVEN** Web 页面可访问但没有有效认证条件
- **WHEN** API/Operator 路径执行成功
- **THEN** Browser 子用例必须标记为 `Blocked(Auth)`
- **AND** 不得因 API 成功将 Browser 标记 Passed
- **AND** 最终报告不得给出需要 Browser 通过的无条件 Go

### Requirement: 外溢边界和 Secret 来源必须由运行态差异证明

正式验收必须 (MUST) 使用实际运行资源的字段前后差异证明业务工作负载未被 Trafficless 临时 Pod 规则污染，并证明目标 pull secret 的内容来自管理面引用 Secret。

#### Scenario: Failover 不修改业务工作负载调度字段

- **GIVEN** 源业务 Deployment 和 StatefulSet 配置了 `affinity`、`nodeSelector` 或 `topologySpreadConstraints`
- **WHEN** 执行完整 Failover 并完成 ScaleUpTarget
- **THEN** 必须分别保存 Failover 前和目标扩容后的业务 workload 模板字段
- **AND** 目标 workload 的 `affinity`、`nodeSelector` 和 `topologySpreadConstraints` 必须与预期恢复语义一致
- **AND** 不得仅凭代码审计或 Trafficless AppRestore modifier 推导该边界通过

#### Scenario: pull secret 内容来自管理面 Secret

- **GIVEN** 目标 Cluster 引用了管理面 dockerconfigjson Secret
- **WHEN** DataSync 或 Drill 向实际恢复 namespace 同步 pull secret
- **THEN** 必须计算管理面和目标 namespace `.dockerconfigjson` 原始字节的 SHA256
- **AND** 两个 SHA256 必须一致
- **AND** Drill namespaceMapping 场景必须证明 Secret 存在于映射后 namespace
- **AND** 必须证明未错误同步到仅作为源 namespace 的位置

#### Scenario: 业务镜像映射不干扰 Trafficless 镜像

- **GIVEN** 业务 `imageSources` 或镜像前缀替换配置与平台 `veleroInstall.imageRegistry` 冲突
- **WHEN** 执行 DataSync 或 Drill data restore
- **THEN** 必须从实际 AppRestore/Velero modifier 和恢复运行结果证明 Trafficless busybox 使用平台 registry
- **AND** 不得仅使用单元测试或静态代码摘录判定通过

### Requirement: Server 到运行资源的链路必须闭环

每个正式 API/Browser 操作必须 (MUST) 保存可关联 Server、Operator 和下游运行资源的证据，静态代码位置只能作为补充。

#### Scenario: Trace ID 可跨层关联

- **GIVEN** Browser 或 API 向 Server 提交操作
- **WHEN** Server 返回响应
- **THEN** 必须保存请求、响应、HTTP 状态和 `X-Trace-Id`
- **AND** 必须保存同一请求对应的 Server handler/service 运行日志
- **AND** 必须关联 Operator/CR、AppRestore 和适用的 Kubernetes/Velero/MinIO 结果

#### Scenario: Trace 未传播到下游层

- **GIVEN** Server 返回了 Trace ID
- **WHEN** Operator 或下游资源没有可关联的 Trace 信息
- **THEN** 报告必须明确标记链路观测缺口
- **AND** 不得仅凭 OpenAPI 或静态代码路径宣称运行链路完整

### Requirement: 历史失败和当前缺陷状态必须分别报告

报告必须 (MUST) 保留每次执行的原始 verdict，同时单独展示缺陷当前状态和修复回归结果。

#### Scenario: 缺陷修复后回归通过

- **GIVEN** 原始 Run 中存在 Failed 用例
- **AND** 新版本回归已经 Passed
- **WHEN** 生成追踪矩阵和最终报告
- **THEN** 原始 Failed 必须保留在原 Run 统计中
- **AND** 修复回归必须作为新的 Run 单独统计
- **AND** 缺陷状态可以标记 Resolved
- **AND** 累计统计不得将历史 Failed 改写为 0

#### Scenario: 边界符合但探索发现产品缺口

- **GIVEN** 一个边界用例的实际行为符合冻结 Expected
- **AND** 执行过程中发现该边界暴露了新的产品缺口
- **WHEN** 判定用例和缺陷
- **THEN** 边界符合性用例必须按 Expected 判定
- **AND** 新产品缺口必须作为独立探索发现或缺陷记录
- **AND** 不得混用一个 verdict 同时表达边界符合和产品缺陷

### Requirement: 自动化门禁必须决定发布建议

正式验收必须 (MUST) 记录 OpenSpec、Harness、测试和 lint 门禁的命令与退出码，并据此给出发布建议。

#### Scenario: 给出无条件 Go

- **GIVEN** 所有范围内强制 E2E 和 Browser 主路径均 Passed
- **AND** 没有未解决 P0/P1
- **WHEN** 生成最终报告
- **THEN** 四个关联 OpenSpec strict 校验必须通过
- **AND** `make harness-preflight`、`make harness-lint`、`make harness-ci` 必须通过
- **AND** `make test` 和 `make lint` 必须通过
- **AND** 只有此时才可以给出无条件 `Go`

#### Scenario: 存在已知门禁债务

- **GIVEN** 产品 E2E 已通过
- **BUT** 任一强制门禁失败或 Browser 主路径 Blocked
- **WHEN** 生成最终报告
- **THEN** 必须完整记录失败命令、退出码和责任边界
- **AND** 不得给出无条件 `Go`
- **AND** 只能按影响给出 `Conditional Go` 或 `No-Go`

#### Scenario: 探索性执行缺少冻结条件

- **GIVEN** 一次历史执行没有冻结源码制品或执行前测试定义
- **WHEN** 引用其结果
- **THEN** 该执行只能标记为探索性证据
- **AND** 不得作为正式版本验收通过的唯一依据
