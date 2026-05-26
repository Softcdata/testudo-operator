# Tasks: Drill (灾备演练) 实施任务

## 1. Operator 实现

### 1.1 CRD Schema 定义
- [x] 1.1.1 新增 `DisasterDrill` CRD：
  - `Spec`: `instanceName`, `groupName`, `targetCluster`, `namespaceMapping`, `skipValidation`, `confirmed`
  - `Status`: `state`, `operationName`, `startTime`, `readyTime`, `executionTime`, `completionTime`, `targetCluster`, `restoreMode`, `message`, `groupProgress`
- [x] 1.1.2 更新 `DisasterOperation.Spec`：
  - 新增 `operationType: drill` 枚举值
  - 新增 `DrillConfig` 结构体
- [x] 1.1.3 运行 `make generate && make manifests`

### 1.2 DisasterDrill Controller (新增)
- [x] 1.2.1 **BDD 测试设计**：
  - 场景 1: 创建 DisasterDrill 自动创建 DisasterOperation
  - 场景 2: 同步 confirmed 到 DisasterOperation
  - 场景 3: 同步 DisasterOperation Status 到 DisasterDrill
  - 场景 4: 删除 DisasterDrill 级联删除 DisasterOperation
- [x] 1.2.2 实现 `Reconcile`：
  - 创建 DisasterOperation (ownerReferences)
  - 同步 `spec.confirmed` 到 DisasterOperation.directive.confirmed
  - 同步 DisasterOperation.Status 到 DisasterDrill.Status
- [x] 1.2.3 实现删除处理：
  - ownerReferences 自动级联删除

### 1.3 DisasterOperation Drill 逻辑
- [x] 1.3.1 **BDD 测试设计**：
  - 场景 1: 校验阶段 - 仅校验基础信息 (Pending → Ready)
  - 场景 2: 等待确认 - Ready 状态保持直到 confirmed=true
  - 场景 3: 执行阶段 - 恢复 + 扩容 (Ready → Executing → Completed)
  - 场景 4: 复用模式 vs 完整恢复模式
  - 场景 5: 命名空间映射
  - 场景 6: 跳过校验

### 1.4 Phase 1: 校验阶段 (自动)
- [x] 1.4.1 实现 `handleDrillPending`：
  - 校验 Instance 状态
  - 校验目标集群可达
  - 检查 DataSync/ResourceSync 是否有最近备份
- [x] 1.4.2 状态转换：Pending → Ready

### 1.5 Phase 2: 执行阶段 (用户确认后)
- [x] 1.5.1 实现 `handleDrillReady`：
  - 监听 `spec.directive.confirmed` 字段
  - confirmed=false 时保持 Ready 状态
  - confirmed=true 时转换到 Executing
- [x] 1.5.2 实现 `handleDrillExecuting`：
  - 创建 AppRestore（始终执行完整恢复）
  - 应用 namespaceMapping
  - 从备份提取副本数
- [x] 1.5.3 复用 `scaleUpTarget` 函数扩容
- [x] 1.5.4 状态转换：Executing → Completed (资源级成功)

### 1.6 测试验证
### 1.6 测试验证
- [ ] 1.6.1 单元测试：DisasterDrill Controller 创建/同步/删除
- [ ] 1.6.2 单元测试：两阶段状态流转
- [ ] 1.6.3 单元测试：namespaceMapping 应用
- [ ] 1.6.4 确保核心逻辑覆盖率 ≥ 80%

### 1.7 容灾组演练支持 (已在 Controller 中实现)
- [x] 1.7.1 ValidateGroupDrill 实现
- [x] 1.7.2 DisasterOperation 分层编排逻辑 (Level-0, Level-1...)
- [x] 1.7.3 子 Operation 创建与状态聚合

## 2. Server 集成

### 2.1 API 支持
- [ ] 2.1.1 新增 DisasterDrill CRUD 接口：
  - CreateDrill
  - GetDrill
  - ListDrills
  - ConfirmDrill (Patch confirmed=true)
  - DeleteDrill
- [ ] 2.1.2 返回演练状态 (Ready/Executing/Completed)

### 2.2 查询接口
- [ ] 2.2.1 GetDrill 返回 Drill 特定信息 (阶段、恢复模式、时间点)
- [ ] 2.2.2 ListDrills 支持按 Instance 过滤

## 3. 文档与验收

### 3.1 文档
- [ ] 3.1.1 更新 API 文档 (DisasterDrill)
- [ ] 3.1.2 编写用户操作指南 (两阶段流程)

### 3.2 验收测试
- [ ] 3.2.1 E2E 测试：完整演练流程 (默认配置)
- [ ] 3.2.2 E2E 测试：完整演练流程 (namespaceMapping)
- [ ] 3.2.3 E2E 测试：完整演练流程 (targetCluster)
- [ ] 3.2.4 验证：创建 DisasterDrill 自动创建 DisasterOperation
- [ ] 3.2.5 验证：校验阶段不执行恢复
- [ ] 3.2.6 验证：等待 confirmed 才执行
- [ ] 3.2.7 验证：演练后同步调度不受影响
- [ ] 3.2.8 验证：演练后 Instance 状态保持 Protected
- [ ] 3.2.9 验证：删除 DisasterDrill 级联删除 DisasterOperation
