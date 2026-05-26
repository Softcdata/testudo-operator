## MODIFIED Requirements

### Requirement: 恢复生命周期控制

AppRestore 控制器必须 (MUST) 在创建 Velero Restore 前使用统一恢复策略输入构建最终 `spec.template` 与 `spec.resourceModifierRules`，而不是仅依赖固定默认值。

#### Scenario: 自动恢复应用实例策略
- **Given** `DisasterInstance.spec.restorePolicy` 已配置恢复策略
- **When** DataSync/ResourceSync/Drill 创建 AppRestore
- **Then** AppRestore 的 template 与 modifier 必须 (MUST) 反映实例策略

#### Scenario: 未配置实例策略时默认回退
- **Given** `DisasterInstance.spec.restorePolicy` 未配置
- **When** 系统构建 AppRestore
- **Then** 系统必须 (MUST) 使用内置默认恢复策略

## ADDED Requirements

### Requirement: 恢复流程必须支持 Class 映射（StorageClass + IngressClass）

系统必须 (MUST) 在 AppRestore 恢复路径支持 SC 与 IngressClass 映射。

#### Scenario: SC 映射生效
- **Given** 实例策略中存在 `storageClassMapping` 的 `source -> target` 映射
- **When** AppRestore 执行恢复
- **Then** 恢复前处理必须 (MUST) 将匹配 PVC/PV 的 `storageClassName` 替换为目标值

#### Scenario: IngressClass 映射生效
- **Given** 实例策略中存在 `ingressClassMapping` 的 `source -> target` 映射
- **When** AppRestore 执行恢复
- **Then** 恢复前处理必须 (MUST) 将匹配 Ingress 的 `spec.ingressClassName` 替换为目标值

#### Scenario: 严格模式下目标 Class 缺失
- **Given** 映射策略开启严格模式（目标不存在或未命中即失败）
- **And** 目标集群不存在映射目标 Class
- **When** 系统准备创建 Velero Restore
- **Then** 系统必须 (MUST) 在执行前失败
- **And** 错误中必须 (MUST) 包含明确失败原因码

### Requirement: 恢复策略来源必须可观测

系统必须 (MUST) 记录恢复策略来源与关键统计，支持审计和复盘。

#### Scenario: 输出策略来源与恢复摘要
- **Given** 一次 AppRestore 已进入恢复流程
- **When** 控制器完成恢复构建
- **Then** 系统必须 (MUST) 记录来源层级（instance/default）
- **And** 记录 SC 与 IngressClass 映射命中/未命中统计
