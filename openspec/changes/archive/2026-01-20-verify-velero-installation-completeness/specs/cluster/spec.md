# Cluster Specification

## ADDED Requirements

### Requirement: Velero 完整性检查 (MUST)

在判定集群中的 Velero 是否安装完成时，Operator (MUST) 必须同时校验 `velero` Deployment 和 `node-agent` DaemonSet 的存在性。仅当两者都存在于目标集群时，才视为 Velero 安装也已完成。这确保了文件系统备份能力（依赖 node-agent）是可用的。

#### Scenario: 检测 node-agent 缺失
- **GIVEN** 目标集群已安装 Velero Deployment
- **BUT** 缺少 `node-agent` DaemonSet
- **WHEN** Operator 执行 Velero 安装检查
- **THEN** 判定为未安装完成
- **AND** 触发 Velero 安装/修复流程
