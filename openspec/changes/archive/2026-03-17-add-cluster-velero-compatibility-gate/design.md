# 设计文档：添加集群时的 Velero/CRD 版本适配校验

## 1. 现状分析

当前 `ClusterReconciler` 的创建主链路为：

1. 鉴权与连通性初始化（kubeconfig 或 token）。
2. 检查 Velero 是否安装；未安装则执行安装。
3. 读取 K8s 版本与节点信息。
4. 通过 `ServerStatusRequest` 获取 Velero 版本并写入 `status.veleroVersion`。
5. 置 `status=Ready`。

缺口：

- 仅“读到版本”但未“判定版本是否受支持”。
- 仅“探测 CRD 可访问”但未“校验关键 CRD 的版本语义是否兼容”。
- 添加集群阶段缺少稳定失败语义，server/web 无法精准报错。

## 2. 设计目标

1. 在添加集群阶段形成确定性门禁：版本不兼容立即失败。
2. 输出稳定、可机器识别的失败语义（`status.reason`）。
3. 不引入 CRD schema 变更，最小化改动。
4. 对后续健康巡检保持一致行为（同一套兼容规则）。

## 3. 方案设计

### 3.1 兼容策略模型

新增统一策略定义（建议常量或独立小模块）：

- `SupportedVeleroVersionRange`：受支持版本范围（例如 `>=1.17.0, <1.18.0`）。
- `RequiredVeleroCRDs`：必须存在且版本兼容的 CRD 列表（例如 `backups.velero.io`、`restores.velero.io`、`serverstatusrequests.velero.io`、`backupstoragelocations.velero.io`）。
- `RequiredCRDVersion`：关键版本 `v1`。

说明：

- 版本范围以 semver 解析为准，允许输入 `v1.17.0` 形式。
- CRD 判定基于 `apiextensions.k8s.io/v1` 的 CRD 对象，检查 `spec.versions`（served/storage）。

### 3.2 Reconcile 门禁位置

在现有 `checkVeleroVersion` 成功返回后、`cluster.Status.Status = Ready` 前插入：

1. `checkVeleroVersionCompatibility(serverVersion)`。
2. `checkVeleroCRDCompatibility(remoteClient)`。

任一步骤失败即：

- `status.status=NotReady`
- `status.reason` 设置为稳定枚举
- `status.message` 写入详细差异
- 记录 Warning Event `VeleroCompatibilityFailed`
- 返回错误并重试（保持现有 Reconcile 语义）

### 3.3 错误语义与文案

建议 reason 枚举：

- `VeleroVersionIncompatible`
- `VeleroCRDVersionIncompatible`
- `VeleroCRDCheckFailed`

`status.message` 格式建议：

- 版本不兼容：
  - `velero version incompatible: expected >=1.17.0,<1.18.0, actual v1.14.2`
- CRD 版本不兼容：
  - `velero crd incompatible: backups.velero.io requires served version v1`
- CRD 检测失败：
  - `failed to validate velero crd compatibility: <raw error>`

### 3.4 创建流程失败事件

“添加集群”首轮失败需要可视化结束态：

- 当 `ObservedGeneration==0` 且本次判定为兼容性失败时，发射 `TaskFinished(Failed)`。
- Message 直接复用 `status.message`，避免 server/web 二次拼接歧义。

### 3.5 跨仓库联动建议

- `disaster-server`：
  - 识别上述 `reason` 并映射为“添加集群失败”业务错误码。
  - 在创建轮询接口中优先透传 `status.message`。
- `cluster-disaster-web`：
  - 针对 reason 展示分类型引导（升级 Velero / 修复 CRD）。
  - 保留“复制详细信息”能力。

## 4. 风险与缓解

### 4.1 风险：历史集群被新门禁判为不兼容

缓解：

- 首版只在创建与编辑流程强制报错；巡检失败仍置 `NotReady` 但不触发创建事件。
- 在发布说明中明确支持矩阵。

### 4.2 风险：版本字符串解析失败

缓解：

- 统一 `normalizeVersion` 逻辑（去前缀 `v`，兼容 metadata）。
- 解析失败归类 `VeleroVersionIncompatible` 并附原始字符串。

### 4.3 风险：RBAC 导致无法读取 CRD

缓解：

- 明确归类为 `VeleroCRDCheckFailed`，阻断 Ready，避免误判成功。

## 5. 测试设计（BDD）

1. `velero version` 不在支持范围：
   - `Cluster` 最终应为 `NotReady`，`reason=VeleroVersionIncompatible`。
2. Velero 版本兼容但关键 CRD 缺失：
   - `Cluster` 最终应为 `NotReady`，`reason=VeleroCRDVersionIncompatible`。
3. Velero 版本兼容且 CRD 兼容：
   - `Cluster` 可进入 `Ready`。
4. 读取 CRD 失败（权限/连接）：
   - `Cluster` 最终应为 `NotReady`，`reason=VeleroCRDCheckFailed`。
5. 创建流程兼容性失败：
   - 必须产出 `TaskFinished(Failed)` 结构化事件。
