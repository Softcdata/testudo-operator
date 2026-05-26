---
name: implement-group-sync-wait
---

# 实施任务

## 任务列表

- [x] Task 1: 修改 handleGroupOperation 添加 wait-completion 标签
- [x] Task 1: 修改 handleGroupOperation 添加 wait-completion 标签 (策略调整：强制所有 Sync 等待)
- [x] Task 2: 重构 handleSync 实现等待逻辑
- [x] Task 3: 添加超时检测
- [x] Task 4: 验证修复

---

## Task 1: 修改 handleGroupOperation 添加 wait-completion 标签

**状态**: ✅ 已完成

**文件**: `internal/controller/disasteroperation/controller.go`

**变更**: 在 `handleGroupOperation` 方法中，创建子 Operation 时添加 `wait-completion` 标签。

**代码位置**: 第 1341-1345 行

---

## Task 2: 重构 handleSync 实现等待逻辑

**状态**: 🔲 待实施

**文件**: `internal/controller/disasteroperation/controller.go`

**变更**:
1. 检测 `wait-completion` 标签
2. 如果为 true，触发同步后不立即完成，而是等待 DataSync/ResourceSync 状态变为 Ready
3. 使用 RequeueAfter 轮询检查状态

---

## Task 3: 添加超时检测

**状态**: 🔲 待实施

**变更**:
1. 在 handleSync 的等待循环中检查是否超时
2. 超时则标记 Operation 为 Failed

---

## Task 4: 验证修复

**状态**: 🔲 待实施

**测试场景**:
1. 创建两层串行组
2. 执行 sync-resource 操作
3. 验证 Level-0 的 ResourceSync 完成后，Level-1 才开始
