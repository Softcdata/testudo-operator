# Change: 集群镜像源与基础配置镜像映射（实例动态读取）

## Why
异地容灾场景中，镜像仓库替换策略属于“主备关系级”配置，而不是“单实例级”配置。  
将映射放在 `DisasterInstance` 会导致同一 `DisasterConfig` 下多个实例重复维护同一规则，配置漂移风险高，且运维审计分散。  
经讨论决定将镜像映射统一下沉到 `DisasterConfig`，由实例在恢复前动态读取，以确保同一基础配置下行为一致且可集中治理。

## What Changes
### 1. Cluster 继续承载镜像源目录
- 保留 `Cluster.spec.imageSources` 作为镜像源目录。
- 每个镜像源保持 `name + registry` 结构，`name` 在同集群内唯一。

### 2. 镜像映射入口从 Instance 下沉到 DisasterConfig
- 新增 `DisasterConfig.spec.imageRewrite` 作为唯一配置入口，字段包含：
  - `enabled`
  - `applyTo`（首期固定支持 `resourceSync`、`drill`）
  - `unmatchedPolicy`（`Fail | Keep`，默认 `Fail`）
  - `mappings[]`（`sourceImageSource -> targetImageSource`）
- `DisasterInstance` 不再承载镜像映射配置入口。

### 3. 实例按引用配置动态读取映射
- 在 ResourceSync 与 Drill 的资源恢复构建阶段，按 `instance.spec.config` 读取最新 `DisasterConfig.spec.imageRewrite`。
- 每次进入恢复构建阶段都重新读取，不缓存历史映射快照。
- 映射方向按当前主备角色计算，确保 failover 后仍按当前 source/target 语义替换。

### 4. Restore 构建链路应用镜像替换
- 在 `ResourceModifierRules` 中覆盖：
  - `deployments.apps`
  - `statefulsets.apps`
  - `containers[*]`
  - `initContainers[*]`
- 对命中 `source.registry` 前缀的镜像替换为 `target.registry`。

### 5. DataSync Trafficless 语义保持隔离
- DataSync 继续只使用 `TrafficlessConfig.Image`，不复用 `DisasterConfig.spec.imageRewrite`。

### 6. Server / Web 配套调整
- Server：
  - Cluster API 继续维护 `imageSources`。
  - DisasterConfig API 新增 `imageRewrite` 字段的创建、更新、查询与校验。
  - Instance API 不再作为 `imageRewrite` 配置入口。
- Web：
  - 在“容灾基础配置（DisasterConfig）”页面维护镜像映射。
  - 实例页面展示关联配置与生效策略，不再单独录入映射。

## Impact
- 受影响规范：
  - `openspec/changes/add-image-source-mapping/specs/cluster/spec.md`
  - `openspec/changes/add-image-source-mapping/specs/instance-image-rewrite/spec.md`
  - `openspec/changes/add-image-source-mapping/specs/restore-builder/spec.md`
- 受影响代码（实施阶段）：
  - `pkg/apis/disaster/v1/disasterconfig_types.go`
  - `internal/controller/restore/builder.go`
  - `internal/controller/disasteroperation/*`
  - `internal/controller/resourcesync/*`
- 跨仓库影响：
  - `disaster-server`：`disaster_config`、`disaster_instance`、`disaster_cluster` 相关 API
  - `cluster-disaster-web`：配置页与实例页交互
  - `disaster-system-chart`：文档说明（如需新增默认字段注释）

## Risks
- 配置下沉后，历史实例若仍提交实例级映射，可能出现“配置入口认知不一致”。
- 配置更新后全量实例行为同步变化，若变更误配会放大影响面。

## Mitigation
- 默认 `unmatchedPolicy=Fail`，在恢复前确定性失败并给出未命中镜像明细。
- 事件与状态增加“读取到的配置版本/摘要”与“命中统计”，支持审计。
- 补齐测试：多容器、initContainer、角色切换、配置更新后动态生效。
