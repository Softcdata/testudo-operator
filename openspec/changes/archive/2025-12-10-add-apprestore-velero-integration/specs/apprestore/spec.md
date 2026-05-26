## 新增需求

### 需求：AppRestore 管理
系统必须提供 AppRestore 和 Velero Restore 资源之间的一对一管理关系。

#### 场景：创建 AppRestore
- **当** 用户创建一个 AppRestore 资源时：
  - 系统必须验证 AppRestore 的字段是否完整，包括名称、集群、命名空间、备份源等。
  - 系统必须检查对应的 Velero Restore 是否已存在，避免重复创建。
  - 系统必须根据 AppRestore 的配置生成一个 Velero Restore 资源。
  - 系统必须将 Velero Restore 的名称和状态记录到 AppRestore 中。

#### 场景：同步状态
- **当** Velero Restore 资源的状态发生变化时：
  - 系统必须监听 Velero Restore 的状态更新事件。
  - 系统必须将 Velero Restore 的状态同步到对应的 AppRestore 资源中。
  - 如果 Velero Restore 状态为失败，系统必须记录失败原因并更新 AppRestore 的状态为失败。
  - 如果 Velero Restore 状态为成功，系统必须更新 AppRestore 的状态为成功。

#### 场景：取消恢复
- **当** 用户取消一个正在进行的 AppRestore 时：
  - 系统必须检查 AppRestore 的当前状态是否允许取消。
  - 系统必须终止对应的 Velero Restore 操作。
  - 系统必须更新 AppRestore 的状态为已取消。
  - 系统必须记录取消操作的时间和原因。

#### 场景：标签管理
- **当** 创建或更新 AppRestore 时：
  - 系统必须为 AppRestore 自动生成以下标签：
    - 名称：唯一标识 AppRestore 的名称。
    - 集群：标识 AppRestore 所属的集群。
    - 命名空间：标识 AppRestore 所属的命名空间。
    - 备份源：标识 AppRestore 恢复的备份来源。
    - 状态：标识 AppRestore 的当前状态（进行中、成功、失败、已取消）。
    - 时间：记录 AppRestore 的创建时间和最后更新时间。

#### 场景：错误处理
- **当** AppRestore 操作失败时：
  - 系统必须记录详细的错误日志，包括错误原因和上下文信息。
  - 系统必须通知相关用户或管理员，提示恢复操作失败。
  - 系统必须提供重新尝试的选项，允许用户重新发起恢复操作。
  - 系统必须确保错误日志中包含恢复操作的上下文信息（如关联的 Velero Restore 名称和状态）。