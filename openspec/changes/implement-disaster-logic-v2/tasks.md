# Tasks: V2.0 业务逻辑与 Server 接口开发

## Phase 1: Operator 功能补全
- [ ] 1.1 **DataSync 模式扩展**:
  - [ ] 1.1.1 支持 `External` 模式：Manual Trigger 逻辑优化。
  - [ ] 1.1.2 支持 `SharedStorage` 模式：跳过 AppBackup，仅做状态占位。
  - [ ] 1.1.3 实现 `NFSMonitor` 逻辑，更新 connectivity status。
- [ ] 1.2 **DisasterOperation 增强**:
  - [ ] 1.2.1 实现 `Drill` (演练) 模式逻辑分支。
  - [ ] 1.2.2 实现 `External Confirm` 等待逻辑 (在 FinalSync 步骤)。
- [ ] 1.3 **Group Operation**:
  - [ ] 1.3.1 验证并完善 Group 级别的 Failover 编排逻辑。

## Phase 2: Server 接口规范 (V2)

### 2.1 数据模型定义 (DTO)
需定义符合 UI 需求的 DTO 结构，整合后端逻辑。

- **Common DTOs**:
  ```go
  type OptionDTO struct {
      Label string `json:"label"`
      Value string `json:"value"`
      Meta  map[string]string `json:"meta,omitempty"` // 扩展字段
  }
  ```

- **DisasterConfigDTO**:
  - 参考 `internal/apis/disaster_config/v1/types.go`，确保包含 `Spec` 和 `Status`。

### 2.2 模块: 容灾基础配置 (Configs)
- [ ] 2.2.1 **Create API**: `POST /api/v2/configs`
  - 校验逻辑：Cluster/Storage 必须存在；互斥性检查。
- [ ] 2.2.2 **List API**: `GET /api/v2/configs`
  - 支持分页、模糊搜索 Name。
- [ ] 2.2.3 **Options API**: `GET /api/v2/configs/options`
  - 聚合返回 Clusters, Storages, Policies (下拉框数据源)。

### 2.3 模块: 容灾实例 (Instances)
- [ ] 2.3.1 **Create API**: `POST /api/v2/instances`
  - 接收 ConfigID, Namespace 等参数。
- [ ] 2.3.2 **List API**: `GET /api/v2/instances`
  - DTO 增强：返回列表需包含 ConfigName、Status (Running/Syncing/Error)。
- [ ] 2.3.3 **Namespace Helper**: `GET /api/v2/clusters/{cluster}/namespaces`
  - **[New]** 代理 k8s API，列出指定集群的 Namespaces 供前端选择。

### 2.4 模块: 容灾组 (Groups)
- [ ] 2.4.1 **API 实现**: Implement `List`, `Create`, `Get` for DisasterGroup。
- [ ] 2.4.2 **Candidates**: `GET /api/v2/groups/candidates`
  - 返回未加入任何 Group 的 Instances 列表，防止循环依赖。

### 2.5 模块: 容灾演练 (Drill Orchestration)
- [ ] 2.5.1 **Drill List**: `GET /api/v2/drills`
  - 过滤 `DisasterOperation` 中 `type=drill` 的记录。
- [ ] 2.5.2 **Drill Create**: `POST /api/v2/drills`
  - 映射到 `DisasterOperation` CRD 创建。
  - Payload 需支持高级选项：`TargetCluster`, `NamespaceMappingOverride`。
- [ ] 2.5.3 **Drill Action**: `POST /api/v2/drills/{id}/action`
  - 支持 Stop/Cleanup 操作。

## Phase 3: Server 基础架构实现
- [ ] 3.1 **Router & Middleware**:
  - Setup `/api/v2` router group.
  - 集成 Gin Validator 和 Error Handling。
- [ ] 3.2 **K8s Client Logic**:
  - 封装对各个 V2 CRD (DisasterInstance, DisasterGroup, DisasterOperation) 的 Client 操作。
  - 封装 Namespace Helper 逻辑。

## Phase 5: 验证与集成
- [ ] 5.1 **E2E 流程验证**:
  - [ ] 5.1.1 通过 API 创建 Instance。
  - [ ] 5.1.2 通过 API 触发 Failover 并通过 WS 监控进度。
  - [ ] 5.1.3 验证 External 模式下的 Confirm 流程。

