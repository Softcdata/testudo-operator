# Design: 无 PVC 纯资源 Drill 与备份预检

## 当前问题

当前链路存在两个相互关联的缺口：

1. `DisasterDrillReconciler.handlePending` 没有读取 DataSync/ResourceSync，直接写 `BackupAvailable=true` 和 `RestoreMode=FullRestore`。
2. `DisasterOperationReconciler.handleDrill` 固定初始化 `RestoreResource`、`RestoreData`、`ScaleUp`；直到 `executeDrillRestoreData` 才检查 `DataSync.status.lastBackupName`。

这意味着“明确无 PVC”和“本应存在数据备份但缺失”在 Pending 阶段无法区分，二者都进入 Ready，并在执行阶段以同一个错误失败。

## 分类模型

分类输入只使用同一 DisasterInstance 当前关联的 DataSync 和 ResourceSync 状态。名称来自 `instance.status.dataSyncName` 与 `instance.status.resourceSyncName`，并校验子资源 `spec.instance` 确实指向该实例。

### ResourceSync 前置条件

所有模式都必须满足：

- ResourceSync 对象存在；
- `status.state=Ready`；
- `status.lastBackupName` 去除空白后非空。

原因是 `ResourceOnly` 不是“什么都不恢复”，而是必须从 ResourceSync 备份恢复 Kubernetes 资源，再执行 ScaleUp。

### DataSync 分类真值表

| DataSync 状态 | no-data condition | 最新 history | lastBackupName | 结果 |
| --- | --- | --- | --- | --- |
| `Ready` | `NoDataVolumes=True/NoPVCFound` | `Skipped` | 任意 | `ResourceOnly` |
| `Ready` | 不满足完整 no-data 条件 | 任意 | 非空 | `FullRestore` |
| 非 `Ready` | 任意 | 任意 | 任意 | Invalid |
| `Ready` | 完整 no-data condition | 缺失或非 `Skipped` | 任意 | Invalid |
| `Ready` | 不满足完整 no-data 条件 | 任意 | 空 | Invalid |

完整 no-data condition 与最新 `Skipped` 必须同时成立。condition 已声明当前没有数据卷但 history 不一致时，不允许回退到可能陈旧的 `lastBackupName`；该状态必须失败关闭。

## Ready 阶段模式快照

`DisasterDrill.status.instanceRestoreModes` 保存 `instanceName -> RestoreMode`：

- 实例 Drill 保存一个条目；
- 同质组的聚合 `status.restoreMode` 为对应的 `FullRestore` 或 `ResourceOnly`；
- 混合组的聚合值为 `Mixed`；
- `Mixed` 只描述父 Drill，不参与单实例步骤生成。

用户确认时重新读取同步状态并分类：

- 历史 Ready 对象没有快照时，补齐快照和聚合模式，更新 status 后 requeue；
- 当前分类与快照一致时允许创建 Operation；
- 成员集合变化、模式变化或备份不可用时进入 Failed，不按旧快照继续。

该复核只冻结“是否需要数据恢复”的语义，不冻结具体 backup name。相同模式下产生了更新的成功备份时，Operation 仍可使用最新备份。

## Operation 模式传递

### 实例 Drill

`DisasterDrill` 创建 Operation 时写：

```yaml
spec:
  drillConfig:
    restoreMode: ResourceOnly # 或 FullRestore
```

### 组 Drill

父 Operation 携带 `instanceRestoreModes`。`handleGroupOperation` 创建子 Operation 时：

1. 对父 `DrillConfig` 调用 generated `DeepCopy()`；
2. 从 map 读取当前 instance 的模式；
3. 写入子配置 `restoreMode`；
4. 清空子配置中的 `instanceRestoreModes`；
5. 绝不原地修改父配置或复用同一个指针。

新控制器创建的父 Operation 若缺少任一成员模式，必须失败。为兼容历史或用户直接创建的旧式 Group Operation，完全没有模式 map 时保留原有 `FullRestore` 默认值。

## 步骤生成

`FullRestore`：

```text
RestoreResource -> RestoreData -> ScaleUp
```

`ResourceOnly`：

```text
RestoreResource -> ScaleUp
```

省略步骤，而不是创建 `RestoreData` 后在执行器内部伪装成功。这样 status steps、事件和审计记录都能准确反映实际动作，并从结构上保证不会进入 data AppRestore、trafficless Pod 和 PVR 链路。

未知、空或历史 Operation 模式默认按 `FullRestore` 处理。只有显式 `ResourceOnly` 可以省略数据恢复。

## 预检失败语义

- `BackupAvailable=false`；
- `DisasterDrill.status.state=Failed`；
- `status.reason=BackupUnavailable`；
- message 包含具体实例和 DataSync/ResourceSync 失败原因；
- 不创建 DisasterOperation；
- `skipValidation` 不绕过该检查。

目标 Cluster readiness 与备份检查独立记录。备份检查通过后才将 `BackupAvailable=true`；不再使用硬编码值。

## 升级与兼容

- `RestoreMode` 仅新增枚举值，不删除 `Reuse` 或 `FullRestore`。
- 新增 status/spec 字段均为 optional。
- 旧 Operation 未携带 `restoreMode` 时继续完整恢复，避免升级导致行为由“恢复数据”静默变为“跳过数据”。
- CRD 必须先于新 Controller 部署，确保 status map 和 Operation config 不被 API Server 裁剪。
- server DTO 使用字符串转换，不需要 Go 结构变化即可透传新聚合模式；server OpenAPI 枚举和注释需要后续同步。

## 不影响边界

- ResourceSync 仍按原逻辑生成资源备份和副本记录；本变更只读取其 status。
- Failover/Reprotect 不读取 `DrillConfig`，步骤表和角色切换不变。
- ResourceSync 的 standby modifier、业务 workload affinity/nodeSelector/topology 及 ScaleUp 行为不变。
- Drill cleanup 不读取恢复模式，仍按 namespaceMapping 选择缩容或删除命名空间。
- DataSync 的 no-PVC 判断及 history 写入不在本变更修改。

## 测试策略

- 纯分类表：三重 no-data 证据、错误 reason、非 Skipped history、状态非 Ready、陈旧 backup name。
- Pending 预检：ResourceOnly Ready、FullRestore Ready、普通数据备份缺失、ResourceSync 备份缺失、`skipValidation=true` 仍失败。
- 确认阶段：模式快照透传；状态漂移时拒绝执行；历史 Ready 对象补齐快照。
- Operation：ResourceOnly 仅生成两个步骤且不创建 data AppRestore；FullRestore 保持三个步骤。
- Group：同质与混合聚合；每个子 Operation 获得独立配置和对应模式。
- 生成物：两个 CRD 包含新增字段和枚举，RBAC 允许 Drill Controller 读取 DataSync/ResourceSync。
- 回归：DisasterDrill、DisasterOperation、DataSync、ResourceSync、restore 相关包和全量 `make test`。
