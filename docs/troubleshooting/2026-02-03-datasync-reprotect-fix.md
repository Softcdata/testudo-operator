# Reprotect (反向同步) 期间数据同步失败 - 故障排查与修复

**日期:** 2026-02-03
**状态:** 已解决
**组件:** DataSync (Velero 集成)

## 1. 问题描述
在执行故障切换 (Failover, 集群 A -> 集群 B) 并触发反向保护 (Reprotect, 集群 B -> 集群 A) 后，数据同步功能未能将数据从新的主集群 (集群 B) 同步回新的备用集群 (集群 A)。

**故障现象:**
*   **PVC 警告:** Velero 恢复日志显示 "PersistentVolumeClaim already exists" (PVC 已存在) 和 "in-cluster version is different" (集群内版本不一致)。这是预期行为（为了保留原卷），但引发了数据是否被覆盖的疑虑。
*   **无数据恢复:** 尽管恢复操作显示完成（带警告），但集群 A 上 PVC 中的数据并未更新。
*   **备份为空:** 集群 B (新主集群) 上生成的 Velero Backup 内容为空或失败。
*   **BSL 缺失:** 集群 B 日志显示 "BackupStorageLocation ... not found" (备份存储位置未找到)。

## 2. 根本原因分析 (Root Cause Analysis)
调查发现，该故障主要由以下问题导致（按影响顺序）：

### 2.1. AppBackup 目标集群不匹配 (主要原因)
*   **问题:** `AppBackup` 自定义资源 (CR) 是一个长生命周期的备份配置。Reprotect 后，集群角色互换 (A: 备用, B: 主)。
*   **影响:** `AppBackup` CR 保留了旧配置 (`Spec.Cluster: A`)。触发同步时，它尝试在集群 A (现为备用，无运行中的 Pod) 上执行备份。由于集群 A 上没有运行该应用的工作负载，导致生成了一个**空备份**（仅包含元数据，无 Pod 卷数据）。
*   **修复:** 更新 `DataSync` 控制器，使其能检测集群方向变化，并将 `AppBackup.Spec.Cluster` 自动更新为新的主集群 (集群 B)。

### 2.2. 新主集群缺失备份存储位置 (BSL)
*   **问题:** 即使修复了目标集群指向 (修复 2.1)，备份仍然失败，因为集群 B 上未配置所需的 `BackupStorageLocation`。
*   **细节:** 动态应用/确保 BSL 存在的逻辑仅存在于 `AppBackup` 控制器的 `PendingHandler` 中。由于该 `AppBackup` 此时已处于 `Ready` 状态，该逻辑被跳过。
*   **修复:** 增强 `AppBackup` 控制器的 `ReadyHandler`，在执行任何 `Backup` (备份) 或 `Retry` (重试) 动作前，显式调用 `ensureBSL` (应用存储仓库逻辑)，确保无论状态如何转换，目标集群上都存在 BSL。

### 2.3. PVC 不可变性冲突
*   **问题:** `AppRestore` 规范使用了 `ExistingResourcePolicy: update`。
*   **影响:** Velero 尝试更新集群 A 上已存在的 PVC 对象。由于 PVC 包含不可变字段（如 `volumeName`），导致 PVC 验证失败。如果不修复此问题，即使备份正常，恢复也会被阻止。
*   **修复:** 将 `ExistingResourcePolicy` 更改为 `None` (默认值)，以跳过 PVC 对象更新，同时允许数据恢复流程继续。

## 3. 其他改进与预防措施

### 3.1. 移除 Pod OwnerReferences (预防性修复)
*   **背景:** 即使备份和 PVC 策略都正常，从源集群恢复的 Pod 通常带有指向源 ReplicaSet 的 `ownerReferences`。
*   **潜在风险:** 如果目标集群上不存在对应的 ReplicaSet (UID 不匹配)，Kubernetes GC 可能会将这些恢复的 Pod 视为孤儿并删除，导致数据恢复中断。虽然此次事故的主因是空备份，但为了增强健壮性，我们实施了此防御性修复。
*   **措施:** 在恢复流程中添加 `ResourceModifierRule`，强制从恢复的 Pod 中**移除 `metadata.ownerReferences`**，确保存活为独立 Pod。

## 4. 解决方案摘要

### 4.1. 代码变更
1.  **`internal/controller/datasync/controller.go`**:
    *   **Fix 2.1:** 更新逻辑以在方向变化时修补 `AppBackup.Spec.Cluster`。
    *   **Fix 2.3:** 设置 `ExistingResourcePolicy: ""` (None)。
    *   **其他:** 在恢复前添加 `cleanupTrafficlessPods` 以确保环境干净；添加 `ResourceModifierRules`。

2.  **`internal/controller/appbackup/appbackup_ready.go`**:
    *   **Fix 2.2:** 添加 `ensureBSL` 辅助方法；在 `Backup`、`Retry`、首次备份及 `Schedule` 创建步骤前调用 `ensureBSL`。

### 4.2. 验证结果
修复后，反向同步工作流如下：
1.  **Reprotect**: `AppBackup` 自动指向集群 B。
2.  **Backup**: 控制器在集群 B 上自动创建 BSL 并执行备份 (包含 Pod 数据)。
3.  **Restore**: 集群 A 恢复 Pod (无 OwnerRef)，并正确跳过 PVC 冲突，完成数据覆盖。

## 5. 结论
此次故障的核心在于**集群角色切换后的配置状态未能自动跟随** (AppBackup Cluster & BSL)。通过完善控制器的状态感知能力和依赖检查逻辑，彻底解决了该问题。
