# 变更: 统一 V2 日志标准 (Unify V2 Logging Standards)

## Why
当前，V2 控制器（`DisasterInstance`, `DataSync`, `ResourceSync`, `DisasterOperation`）使用 **中文** 发射日志，并且未在 logger 上下文中包含 **TraceID**。这违反了 `operator-best-practices.md`（要求 TraceID 传播），并且与 V1 控制器（使用英文和 TraceID）以及通用的云原生标准不一致。

## What Changes
- 重构所有 V2 控制器，将日志消息统一改为 **英文**。
- 实现 **TraceID 全链路传递**（详见 `design.md`）：
  - **提取**: 从 CRD Annotation 提取。
  - **注入**: 注入 Context 和 Logger。
  - **传播**: 在创建子资源时向下游传递。
- 标准化日志键值对（统一为 `trace_id`）。
- 更新 `development-standards` 中的严格验证规则。

## Impact
- **受影响规范**: `development-standards`
- **受影响代码**: 
  - `internal/controller/disasterinstance/controller.go`
  - `internal/controller/datasync/controller.go`
  - `internal/controller/resourcesync/controller.go`
  - `internal/controller/disasteroperation/controller.go`
