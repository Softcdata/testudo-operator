## ADDED Requirements

### Requirement: DisasterConfig 必须支持基础配置级镜像源映射
系统必须 (MUST) 允许在 `DisasterConfig.spec.imageRewrite` 中声明镜像源映射关系，以控制容灾恢复时镜像仓库替换行为。

#### Scenario: 启用基础配置映射
- **Given** 一个 `DisasterConfig` 配置了 `imageRewrite.enabled=true`
- **And** 配置了至少一条 `sourceImageSource -> targetImageSource` 映射
- **When** 系统执行容灾恢复相关流程
- **Then** 系统必须 (MUST) 按映射规则替换命中的镜像前缀

#### Scenario: 映射引用不存在的镜像源
- **Given** `DisasterConfig.spec.imageRewrite` 的映射引用了 `Cluster` 中不存在的镜像源别名
- **When** API Server 接收创建或更新请求
- **Then** 系统必须 (MUST) 拒绝该请求
- **And** 返回可读错误，指出缺失的镜像源别名

### Requirement: 未命中策略必须可配置且可预测
系统必须 (MUST) 提供未命中策略 `unmatchedPolicy` 来定义“未匹配到映射的镜像”如何处理。

#### Scenario: unmatchedPolicy 为 Keep
- **Given** `unmatchedPolicy=Keep`
- **When** 某镜像未命中任何映射
- **Then** 系统必须 (MUST) 保留原镜像引用
- **And** 继续后续恢复流程

#### Scenario: unmatchedPolicy 为 Fail
- **Given** `unmatchedPolicy=Fail`
- **When** 某镜像未命中任何映射
- **Then** 系统必须 (MUST) 在创建恢复任务前失败
- **And** 状态与事件中必须 (MUST) 记录未命中的镜像信息
