# Change: 添加集群时增加 Velero/CRD 版本适配门禁

## Why

当前 `Cluster` 添加流程会检查 Velero 是否安装，并读取 `ServerStatusRequest` 的 `serverVersion` 写入 `status.veleroVersion`，但缺少“是否受平台支持”的门禁判定。

这会导致以下问题：

- 目标集群 Velero 版本过低/过高时，集群仍可能被标记为 `Ready`，真实风险延迟到备份/恢复执行时才暴露。
- Velero CRD 若为不兼容版本（或关键 CRD 缺失），当前流程缺少明确阻断，容易出现“注册成功但运行失败”的体验。
- 上层 `disaster-server` / `cluster-disaster-web` 难以在“添加集群”阶段给出确定性失败原因。

## Goals

- 在“添加集群”主链路中增加 **Velero 版本兼容校验** 与 **Velero CRD 版本兼容校验**。
- 版本或 CRD 不兼容时，`Cluster` 必须进入 `NotReady`，并输出可读的 `reason/message`，用于上层直接报错。
- 保持现有安装逻辑：未安装时仍可自动安装 Velero；安装后必须通过兼容校验才可 `Ready`。
- 输出稳定的错误语义，便于 server/web 做精确提示。

## Non-Goals

- 不在本提案中引入自动升级/自动修复“已安装但版本不兼容”的 Velero 集群。
- 不修改 `Cluster` CRD 的 schema（复用现有 `status.reason/status.message`）。
- 不改变现有 `Cluster` 创建 API 入参结构。

## What Changes

1. 在 `ClusterReconciler` 增加统一兼容校验入口（建议：`checkVeleroCompatibility`）。
2. 增加 Velero 版本范围校验：
   - 解析 `status.serverVersion`（语义化版本）。
   - 与 Operator 支持矩阵比较（例如：`>=1.17.0, <1.18.0`，以实现常量为准）。
3. 增加 Velero CRD 版本校验：
   - 校验关键 CRD 是否存在（如 `backups.velero.io`、`restores.velero.io`、`serverstatusrequests.velero.io` 等）。
   - 校验关键 CRD 是否对 `velero.io/v1` 提供 `served=true`，并满足预期存储版本策略。
4. 兼容校验失败时：
   - 设置 `cluster.status.status=NotReady`；
   - 设置 `status.reason` 为稳定枚举（如 `VeleroVersionIncompatible` / `VeleroCRDVersionIncompatible` / `VeleroCRDCheckFailed`）；
   - `status.message` 写入 `expected + actual + remediation`。
   - 发射 `Warning` 事件 `VeleroCompatibilityFailed`。
5. “创建集群”场景下若失败，补发结构化 `TaskFinished(Failed)`，确保前端在创建流程内直接感知失败。

## Impact

- `disaster-operator`：新增版本门禁与 CRD 版本门禁逻辑，完善错误语义。
- `disaster-server`：可基于 `Cluster.status.reason` 将“添加集群失败”映射为明确错误码/错误文案。
- `cluster-disaster-web`：可直接展示“版本不兼容/CRD 不兼容”的可操作提示，而不是泛化失败。

## Risks

- 历史集群如果 Velero 版本不满足新策略，可能从 `Ready` 变为 `NotReady`。
- 不同发行版 Velero 的版本字符串可能带前缀或构建元信息，需做健壮解析。

## Mitigations

- 采用“可配置或常量化”的兼容矩阵，避免散落硬编码。
- 兼容版本字符串前缀（如 `v1.17.0`）与元信息解析。
- 错误消息明确给出修复建议（升级 Velero 或修复 CRD 版本）。
