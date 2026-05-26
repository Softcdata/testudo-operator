# 设计说明：实例级恢复策略与 Class 映射

## 1. 目标

为 `DataSync`、`ResourceSync`、`Drill` 的自动恢复路径提供统一、可审计的实例级恢复参数模型，重点补齐：

1. Velero Restore 细粒度参数控制。
2. 跨集群 `StorageClass/IngressClass` 映射。
3. 容灾切换时 Pod 就绪校验默认策略的实例级配置能力。

## 2. 配置模型

### 2.1 实例级唯一入口

恢复策略仅在 `DisasterInstance.spec.restorePolicy` 配置，不在 `DisasterConfig` 提供同类字段。

建议结构（逻辑分组）：

- `resourceSelection`
- `execution`
- `storageClassMapping`
- `ingressClassMapping`

### 2.2 优先级模型

`instance restorePolicy > built-in default`

注意：`operation override` 仅预留，不在本次实现。

## 3. 执行链路

### 3.1 统一构建入口

在 `restore/builder` 增加统一策略解析与构建函数，输出：

1. 最终 `velero.RestoreSpec`
2. 最终 `ResourceModifierRules`（含 SC/IngressClass 映射 patch）
3. 策略摘要（来源层级、命中统计）

### 3.2 三条调用链统一接入

- `DataSync` 构建 AppRestore
- `ResourceSync` 构建 AppRestore
- `DisasterOperation (Drill)` 构建 AppRestore

## 4. Class 映射实现策略

### 4.1 规则表达

- `storageClassMapping.mappings[]`: `source -> target`，支持可选命名空间限定。
- `ingressClassMapping.mappings[]`: `source -> target`，支持可选命名空间限定。

每类映射均支持：

- `unmatchedPolicy=Keep|Fail`
- 严格模式下的目标类存在性校验

### 4.2 转译方式

将规则转译为 `resourceModifierRules`：

- SC 映射：
  - `persistentvolumeclaims` -> `/spec/storageClassName`
  - `persistentvolumes` -> `/spec/storageClassName`
- IngressClass 映射：
  - `ingresses.networking.k8s.io` -> `/spec/ingressClassName`
  - 对旧格式 ingress 可选补充注解 patch（`kubernetes.io/ingress.class`）

### 4.3 失败策略

- `Keep`：未命中保持原值。
- `Fail`：未命中、冲突、非法映射或目标类不存在时，恢复构建阶段直接失败。

## 5. 向后兼容

1. 未配置 `DisasterInstance.spec.restorePolicy` 时，维持当前默认行为。
2. DataSync 的 trafficless 规则继续保留，并高于通用恢复策略的潜在冲突项。
3. 未配置 `DisasterInstance.spec.skipPodReadyCheck` 时，沿用当前就绪校验行为。

## 6. 容灾切换就绪校验策略

### 6.1 实例级默认字段

在 `DisasterInstance.spec` 增加 `skipPodReadyCheck`：

- `true`：跳过容器就绪验证，仅要求副本配置已下发。
- `false`：启用容器就绪验证，检查 `readyReplicas` 达标。

### 6.2 操作级覆盖优先级

`DisasterOperation` 入参可覆盖实例默认策略，优先级为：

`DisasterOperation 输入 > DisasterInstance.spec.skipPodReadyCheck`

### 6.3 组操作一致性

Group 操作创建子 `DisasterOperation` 时，必须透传就绪校验相关参数，保证父子执行语义一致。

## 7. 可观测性

在 AppRestore 事件或状态中输出：

1. 策略来源（instance/default）。
2. SC 映射命中/未命中数量。
3. IngressClass 映射命中/未命中数量。

## 8. 测试重点

1. 实例策略与默认值合并测试。
2. SC/IngressClass 映射转译正确性与严格模式失败测试。
3. DataSync/ResourceSync/Drill 三条恢复链路参数一致性测试。
4. 跨集群恢复（或集成模拟）中 Class 映射生效测试。
5. 父 Group 操作与子 Operation 的就绪校验参数透传一致性测试。
