# 任务清单：增强结构化事件发射

## 核心开发
- [x] **通用 Helper 实现**:
    - [x] 在 `pkg/helper/` 下创建 `event_reporter.go`。
    - [x] 实现结构化消息组装逻辑。
    - [x] 实现 `metav1.Time` 差值计算并格式化为字符串。

## 控制器改造
- [x] **AppBackup 控制器**:
    - [x] 在 `internal/controller/appbackup/appbackup_ready.go` 中，识别 Velero Backup 状态变更点。
    - [x] 确保在状态同步逻辑中，仅在状态转为终态时发射一次事件（避免重复发射）。
- [x] **AppRestore 控制器**:
    - [x] 在 `internal/controller/apprestore/apprestore_controller.go` 中，在阶段变更为 `PhaseSucceeded`/`PhaseFailed` 的分支增加事件调用。

## 验证与测试
- [x] **单元测试**:
    - [x] 为 `EventReporter` 编写测试，验证不同时间差的格式化输出。
- [x] **集成验证**:
    - [x] 启动 Operator，执行一次应用备份。
    - [x] 使用 `kubectl get events` 确认输出格式是否完全符合：`[Task: ...] [Status: ...] [Duration: ...] [Cluster: ...] [User: ...] ...`
