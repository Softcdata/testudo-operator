## ADDED Requirements

### Requirement: RestoreBuilder 必须统一解析实例恢复策略

RestoreBuilder 必须 (MUST) 提供统一的实例策略解析能力，并输出可直接用于 AppRestore 的最终恢复配置。

#### Scenario: 应用实例策略并回退默认值
- **Given** `DisasterInstance.spec.restorePolicy` 已部分配置
- **When** RestoreBuilder 构建恢复参数
- **Then** 系统必须 (MUST) 使用实例配置覆盖对应字段
- **And** 对未配置字段回退到内置默认值

### Requirement: RestoreBuilder 必须支持 Class 映射转译

RestoreBuilder 必须 (MUST) 将 SC/IngressClass 映射策略转译为恢复前可执行规则，并在构建阶段执行必要校验。

#### Scenario: SC 与 IngressClass 映射转译
- **Given** `storageClassMapping` 与 `ingressClassMapping` 已配置映射
- **When** RestoreBuilder 构建最终 `ResourceModifierRules`
- **Then** 系统必须 (MUST) 生成 PVC/PV 与 Ingress 维度的 class patch 规则

#### Scenario: 严格模式映射校验失败
- **Given** 严格模式开启且映射目标 Class 不存在
- **When** RestoreBuilder 构建恢复配置
- **Then** 系统必须 (MUST) 返回构建错误
- **And** 调用方不得 (MUST NOT) 继续创建 AppRestore

### Requirement: RestoreBuilder 不得破坏 DataSync trafficless 语义

在引入统一实例策略后，DataSync 的 trafficless 语义必须 (MUST) 保持不变。

#### Scenario: DataSync 恢复保持 trafficless 关键规则
- **Given** DataSync 执行数据恢复
- **When** RestoreBuilder 构建 AppRestoreSpec
- **Then** 系统必须 (MUST) 保留 trafficless 关键 patch 规则
- **And** 不得 (MUST NOT) 因策略应用导致 Pod 接流量
