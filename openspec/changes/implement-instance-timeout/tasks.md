# Tasks: 容灾实例操作超时机制 (Instance Operation Timeout)

## 1. Schema 定义 (CRD)
- [x] 1.1 **DisasterInstance 更新**:
    - [x] 1.1.1 增加 `Spec.OperationTimeoutMinutes` (int32, optional)
    - [x] 1.1.2 运行 `make generate && make manifests`
- [x] 1.2 **DisasterOperation 更新**:
    - [x] 1.2.1 增加 `Spec.TimeoutMinutes` (int32, optional)
    - [x] 1.2.2 运行 `make generate && make manifests`

## 2. Controller 逻辑实现
- [x] 2.1 **初始化阶段 (Timeout Inheritance)**:
    - [x] 2.1.1 在 Operation 初始化时（Pending 状态），检查 `TimeoutMinutes`
    - [x] 2.1.2 如果为空 (0)，从关联的 DisasterInstance 读取 `OperationTimeoutMinutes` 并更新到 Spec
- [x] 2.2 **超时检查 (Timeout Check)**:
    - [x] 2.2.1 在 `handleSync` 中增加整体超时检查
    - [x] 2.2.2 在 `handleFailover` 的步骤执行循环中增加超时检查
    - [x] 2.2.3 在 `handleReprotect` 的步骤执行循环中增加超时检查
    - [x] 2.2.4 超时发生时：
        - 设置 `Status.State = Failed`
        - 设置 `Status.Message` 包含超时原因
        - 记录 Event (Warning/Timeout)

## 3. 验证与测试
- [ ] 3.1 **编译验证**: 运行 `make` 确保无编译错误
- [ ] 3.2 **单元测试**:
    - [ ] 3.2.1 测试超时继承逻辑
    - [ ] 3.2.2 测试步骤超时逻辑
- [ ] 3.3 **E2E 验证**:
    - [ ] 3.3.1 模拟长时间运行的 CheckReplicas 触发超时

## 4. Server 集成 (可选/后续)
- [ ] 4.1 **API 支持**: 确认 Server API 是否允许透传 `timeoutMinutes`
