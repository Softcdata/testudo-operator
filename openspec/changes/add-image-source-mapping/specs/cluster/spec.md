## ADDED Requirements

### Requirement: Cluster 必须支持镜像源目录配置
系统必须 (MUST) 允许在 `Cluster` 资源中声明一个或多个镜像源定义，以供实例级镜像映射复用。

#### Scenario: 配置多个镜像源
- **Given** 用户创建或更新一个 `Cluster`
- **When** 在 `spec` 中提供多个镜像源定义
- **Then** 系统必须 (MUST) 持久化该镜像源列表
- **And** 每个镜像源必须 (MUST) 包含可读别名与仓库前缀

#### Scenario: 镜像源别名冲突
- **Given** 用户在同一个 `Cluster` 中配置了重复的镜像源别名
- **When** API Server 接收该请求
- **Then** 系统必须 (MUST) 拒绝该请求
- **And** 返回明确的校验错误信息
