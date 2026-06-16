## ADDED Requirements

### Requirement: Restore builder MUST runtime-compile rewriteImage actions
Restore builder 在 ResourceSync、Drill 和 Failover 的资源恢复构建阶段必须 (MUST) 对命中当前操作的 `rewriteImage` 动作执行运行时编译。

#### Scenario: ResourceSync 构建时读取源集群当前镜像
- **Given** 一个 `DisasterInstance` 声明了 `bulkModifierActions[].action=rewriteImage`
- **And** 当前操作为 `resourceSync`
- **And** 源集群当前 Deployment 镜像为 `10.11.11.1:5000/blueking/app:v1.31.0`
- **When** Restore builder 构建本次 AppRestore
- **Then** Restore builder 必须 (MUST) 读取源集群当前镜像
- **And** 必须 (MUST) 生成目标镜像对应的 resource modifier rule
- **And** 该规则必须 (MUST) 写入本次 AppRestore 的 ResourceModifier 执行输入

#### Scenario: Drill 构建时使用当前源镜像而不是旧快照
- **Given** 上一次 ResourceSync 编译镜像为 `10.11.11.1:5000/blueking/app:v1.30.0`
- **And** 本次 Drill 前源集群镜像变为 `10.11.11.1:5000/blueking/app:v1.31.0`
- **When** Restore builder 构建 Drill 资源恢复 AppRestore
- **Then** 系统必须 (MUST) 基于 `v1.31.0` 编译镜像重写规则
- **And** 不得 (MUST NOT) 复用上一次 ResourceSync 的完整镜像值

### Requirement: rewriteImage MUST NOT require persistent modifierRuleSnapshot
`rewriteImage` 是运行时编译动作，系统不得 (MUST NOT) 因为该动作没有持久化 `modifierRuleSnapshot` 或 `modifierRuleSnapshotHash` 而失败关闭。

#### Scenario: rewriteImage 无长期快照仍可构建恢复
- **Given** 一个实例只声明了已启用的 `rewriteImage` 动作
- **And** `restorePolicy.modifierRuleSnapshot` 为空
- **And** `restorePolicy.modifierRuleSnapshotHash` 为空
- **When** Restore builder 构建 ResourceSync AppRestore
- **Then** 系统不得 (MUST NOT) 因缺少长期 snapshot 失败关闭
- **And** 必须 (MUST) 在运行时编译镜像规则

#### Scenario: 静态 bulk action 仍保持原有快照要求
- **Given** 一个实例声明了已启用的 `replaceExactValue` 或 `removeKey` 动作
- **And** 缺少对应 `modifierRuleSnapshot` 或 `modifierRuleSnapshotHash`
- **When** Restore builder 构建 AppRestore
- **Then** 系统必须 (MUST) 保持现有失败关闭语义

### Requirement: Restore builder MUST report runtime image rewrite summary
Restore builder 必须 (MUST) 为动态镜像重写编译结果提供可观测摘要。

#### Scenario: AppRestore 或操作状态记录编译摘要
- **Given** Restore builder 成功编译了 `rewriteImage` 动作
- **When** 系统创建本次 AppRestore 或更新操作状态
- **Then** 系统必须 (MUST) 记录生成规则数量
- **And** 必须 (MUST) 记录匹配镜像数量
- **And** 必须 (MUST) 记录未匹配镜像数量
- **And** 必须 (MUST) 记录跳过 forbidden path 数量

#### Scenario: 编译失败返回可诊断错误
- **Given** 动态镜像重写编译遇到冲突或 `unmatchedPolicy=Fail` 未命中
- **When** Restore builder 返回错误
- **Then** 错误必须 (MUST) 包含操作类型、资源标识、镜像路径和关联 bulk action ID
