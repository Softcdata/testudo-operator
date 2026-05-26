# Spec: 集群删除流程

## Requirements

### Deletion State
当集群被标记为删除时，其状态必须变更为 `Deleting`。

### Velero Uninstallation
系统必须支持在删除集群记录时，选择性地卸载目标集群上的 Velero。

#### Scenario: 删除并卸载 Velero
Given 一个已安装 Velero 的受管集群
And 该集群 CR 带有 `testudo.softcdata.com/uninstall-velero: "true"` 注解
When 用户删除该集群 CR
Then Operator 应连接到目标集群并卸载 Velero
And Operator 应移除 Finalizer 允许 CR 删除

#### Scenario: 删除保留 Velero
Given 一个已安装 Velero 的受管集群
And 该集群 CR 没有 `testudo.softcdata.com/uninstall-velero: "true"` 注解
When 用户删除该集群 CR
Then Operator 不应卸载目标集群上的 Velero
And Operator 应移除 Finalizer 允许 CR 删除

#### Scenario: 删除受保护集群
Given 一个被 AppBackup 引用的集群
When 用户尝试删除该集群 CR
Then Operator 应阻止 Finalizer 移除
And 集群状态应保持在 Deleting 或相关阻塞状态
