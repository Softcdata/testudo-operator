---
name: implement-operation-retry
---

# 实施任务

## 任务列表

- [x] Task 1: 修改 CRD 定义 (Types): DisasterOperationSpec, Status, DisasterGroupPolicy
- [x] Task 2: 生成 manifests 并 apply
- [x] Task 3: 修改 DisasterGroup handler 传递 RetryPolicy (Server 端)
- [x] Task 4: 修改 `handleSync` 实现重试逻辑
- [x] Task 5: 验证功能

---

## Task 1: 修改 CRD 定义

**文件**:
- `pkg/apis/disaster/v1/disasteroperation_types.go`
- `pkg/apis/disaster/v1/disastergroup_types.go`

## Task 2: 生成 Manifests

**命令**:
```bash
make manifests
kubectl apply -f config/crd/bases/
```

## Task 3: 修改 Server 端 Handler

**文件**:
- `internal/apis/disaster_group/v1/handler.go`

**变更**:
在创建 `DisasterOperation` CR 时，从 `DisasterGroup` 的 Policy 中读取 `RetryPolicy` 并设置到 Spec 中。

## Task 4: 修改 Controller

**文件**:
- `internal/controller/disasteroperation/controller.go`

**变更**:
重构 `handleSync`，引入重试状态机。
