# 规范：Operator 开发标准

## 概览
本规范定义了 `disaster-operator` 项目中开发 Kubernetes Operator 的标准工作流和最佳实践。

## Requirements
### Requirement: CRD 创建与代码生成
所有新的自定义资源定义 (CRD) 必须 (MUST) 遵循标准化的创建和生成流程。

#### Scenario: 创建新的 CRD
- **Given** 开发者需要引入一个新的 API 资源
- **When** 使用 `kubebuilder create api` 命令生成脚手架（默认沿用当前 GroupVersion）
- **And** 将生成的 API 文件移入 `pkg/apis/<group>/<version>/` 目录中
- **And** 在 `pkg/apis/<group>/<version>/xx_types.go` 中定义 Spec 和 Status 结构体
- **And** 执行 `make generate` 生成 DeepCopy 方法
- **And** 执行 `make manifests` 生成 CRD YAML 文件
- **And** 执行 `hack/update-codegen.sh` 生成 Clientset, Listers, 和 Informers
- **Then** 项目中应包含完整的 API 定义和生成的客户端代码

### Requirement: 控制器实现
控制器逻辑必须 (MUST) 健壮、可靠且易于监控。

#### Scenario: 编写 Reconcile 逻辑
- **Given** 一个新的控制器被创建
- **When** 实现 `Reconcile` 方法
- **Then** 该方法必须是**幂等**的（多次执行产生相同结果）
- **And** 必须包含结构化日志记录（使用 `logr`）
- **And** 应当记录关键指标（Metrics）以确保**可观测性**
- **And** 应当正确处理 Kubernetes 事件（Events），在关键操作（如创建资源、状态变更、错误发生）时记录 Event 以提供上下文

### Requirement: 测试先行 (Test-First Development)
在开发控制器功能时，必须 (MUST) 遵循"测试先行"原则：先设计并实现 BDD 风格的测试用例，再完成 Reconcile 业务逻辑。

#### Scenario: Reconcile 功能开发流程
- **Given** 开发者准备实现一个新的 Reconcile 功能或 Handler
- **When** 开始编码之前
- **Then** 必须先在对应的 `_test.go` 文件中编写 BDD 测试用例（使用 Ginkgo/Gomega）
- **And** 测试用例必须覆盖预期的正常路径和至少一个错误路径
- **And** 确保测试可以运行并失败（红灯阶段）
- **And** 然后实现业务逻辑使测试通过（绿灯阶段）
- **And** 最后进行重构优化（重构阶段）

#### Scenario: OpenSpec 提案规划优先考虑 BDD
- **Given** 开发者正在创建一个新的 OpenSpec 变更提案
- **When** 编写 `tasks.md` 任务列表时
- **Then** 必须将 BDD 测试设计作为优先任务项
- **And** 任务顺序应为：1) 设计测试场景 → 2) 实现测试代码 → 3) 实现业务逻辑 → 4) 验证覆盖率
- **And** `proposal.md` 中应明确列出关键的测试场景

#### Scenario: 测试用例编写规范
- **Given** 开发者编写 BDD 测试用例
- **When** 使用 Ginkgo 框架时
- **Then** 必须使用 `Describe`/`Context`/`It` 结构组织测试
- **And** 测试描述必须清晰表达被测行为（如 "should transition to Ready phase when cluster is reachable"）
- **And** 使用 `BeforeEach` 设置测试 Fixture，确保测试隔离性
- **And** Mock 外部依赖（如 Velero 客户端、远程集群连接）以保证测试的确定性

### Requirement: 结构化任务事件记录 (Structured Task Events)
所有 Operator 控制器在执行耗时较长的后台任务（如 AppBackup, AppRestore, Failover 等）以及 Cluster/Storage 的生命周期管理时，必须 (MUST) 记录遵循统一格式的结构化 Kubernetes Event，以便于 Server 端聚合与展示任务历史。

#### Scenario: 事件标识与过滤 (Mandatory Label)
- **Given** 控制器准备发射一个结构化事件
- **When** 构建 Event 对象时
- **Then** 必须 (MUST) 包含 Label: `testudo.softcdata.com/task-event: "true"`
- **And** Server 端将仅采集带有此 Label 的事件作为任务历史

#### Scenario: 记录结构化事件消息
- **Given** 控制器正在执行一个后台任务
- **When** 任务启动、进度更新或状态结束时
- **Then** 必须 (MUST) 使用 `pkg/helper/event_reporter.go` 中的辅助工具进行上报
- **And** 事件内容必须严格遵循 [Global Event Reporting Standard](../global-events/spec.md) 中的格式定义（包括 Task 命名规范、中文描述等）
- **And** 所有的关键步骤（Started/Finished）都必须成对出现


#### Scenario: 事件防抖 (Event Debouncing)
- **Given** 控制器的 Reconcile 循环被频繁触发
- **When** 资源状态未发生实质性变化时
- **Then** 严禁重复发射相同状态的结构化事件
- **And** 必须在 CRD Status 中维护 `LastEventPhase` 字段来记录上一次发射事件时的状态
- **And** 仅当 `CurrentPhase != LastEventPhase` 时才发射新的结构化事件
- **And** 发射后必须立即更新 Status 中的 `LastEventPhase`

#### Scenario: 任务阶段化发射策略 (Two-Phase Eventing)
- **Given** 一个完整的后台任务生命周期
- **When** 任务正式启动且外部资源（如 Velero Backup）创建成功时
- **Then** 必须发射一个 `Reason: ExecutionStarted` 的事件，Status 设为 `InProgress`
- **When** 任务达到终态（Success/Failed/Canceled）时
- **Then** 必须发射一个 `Reason: ExecutionFinished` 的事件，包含最终的 Status 和计算好的 Duration
- **And** `ExecutionFinished` 事件对于失败任务应使用 `Warning` 类型，成功任务使用 `Normal` 类型

详细规范参见: [Global Event Reporting Standard (V2)](../global-events/spec.md)


### Requirement: 跨命名空间资源级联删除 (Cross-Namespace Cascading Deletion)
当控制器管理的资源位于不同的命名空间（特别是 Velero 资源）时，必须使用 Finalizer 模式来确保级联删除。

#### Scenario: 管理 Velero 资源
- **Given** 控制器需要管理位于不同命名空间（如 `velero`）的资源（如 `Backup`, `Schedule`）
- **When** 定义资源的生命周期管理逻辑
- **Then** 必须 (MUST) 使用 **Finalizer** 模式而不是 `OwnerReference`（因为 `OwnerReference` 不支持跨命名空间）
- **And** 在 CRD 创建时添加 Finalizer
- **And** 在 CRD 删除时（`DeletionTimestamp` 不为空）执行外部资源清理逻辑
- **And** 清理成功后移除 Finalizer

### Requirement: OpenSpec 变更归档与提交
当 OpenSpec 变更提案（Change Proposal）完成并归档时，必须 (MUST) 使用标准化的 Commit Message 提交代码。

#### Scenario: 归档变更提案
- **Given** 一个变更提案的所有任务已完成
- **And** 执行了 `openspec archive <change-name>`
- **When** 提交代码到版本控制系统
- **Then** Commit Message 的标题应为 `feat(<scope>): <summary>`
- **And** Commit Message 的正文应包含已完成任务的详细列表（通常来源于 `tasks.md`）
- **And** 确保 Commit Message 清晰反映了该变更带来的价值和影响

### Requirement: 单元测试覆盖率标准
所有核心控制器（Controller）必须 (MUST) 具备单元测试，且核心业务逻辑（Reconcile loop）的语句覆盖率必须达到 80% 以上。

#### Scenario: 验证新控制器的测试覆盖
- **Given** 开发者提交了一个新的控制器实现
- **When** 运行项目标准的测试套件（如 `go test -cover`）
- **Then** 该控制器的 Reconcile 相关代码覆盖率必须显示为 80% 或更高
- **And** 测试用例必须涵盖至少一个 Mock 错误处理路径（如 K8s API 报错模拟）

#### Scenario: 单元测试覆盖率
- **Given** 开发者提交了新的功能代码或修改了现有逻辑
- **When** 运行单元测试套件
- **Then** 核心模块（如 Cluster 模块）的测试覆盖率必须 (MUST) 达到 **80%** 以上
- **And** 必须覆盖所有错误处理路径和边缘情况

### Requirement: 控制器依赖注入模式
为了提高可测试性，控制器 Reconciler 结构体必须 (MUST) 采用依赖注入模式来访问外部系统或执行命令。

#### Scenario: 重构控制器以支持 Mock
- **Given** 控制器包含直接调用 `os`, `exec` 或底层 `kubeclient` 的硬编码代码
- **When** 进行代码审查或功能开发时
- **Then** 必须将其抽象为接口（如 `CommandExecutor`, `ClientFactory`）并注入到 Reconciler 中


### Requirement: Failover 副本数恢复
系统必须 (MUST) 在 Failover 期间正确恢复目标集群工作负载的副本数。

#### Scenario: 正确记录源集群副本数
Given 一个处于 `Protected` 状态的 `DisasterInstance`
And 源集群有运行中的 Deployment/StatefulSet (副本数 > 0)
When 执行 Failover 操作，进入 `ScaleDownSource` 步骤
Then 系统必须在缩容之前记录所有工作负载的当前副本数到 ConfigMap
And ConfigMap 名称格式为 `replicas-{ResourceSyncName}`
And 记录的副本数必须是缩容前的实际值（非0）。

#### Scenario: 目标集群副本数恢复
Given Failover 的 `ScaleUpTarget` 步骤正在执行
And 存在记录副本数的 ConfigMap
When 系统扩容目标集群的 Deployment/StatefulSet
Then 系统必须从 ConfigMap 读取原始副本数
And 将目标集群工作负载的副本数设置为原始值
And 目标集群的工作负载必须成功启动。

#### Scenario: 保护已记录的副本数不被覆盖
Given Failover 期间源集群副本数已被记录到 ConfigMap
And `FinalSync` 步骤触发了 ResourceSync
When ResourceSync 尝试记录副本数
And 扫描到的副本数为 0 (因为已经缩容)
Then ResourceSync 不得覆盖 ConfigMap 中已有的非零副本数记录。
