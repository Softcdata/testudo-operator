## 1. Spec 与设计
- [x] 1.1 更新 `global-events` 增量规范，明确端到端降噪与聚合约束
- [x] 1.2 在本提案内同步 `disaster-server` 的 `api-standards` 规范语义，不创建独立提案

## 2. Server 修复（A/B/C）
- [x] 2.1 修改 `internal/apis/event/v1/list.go`
- [x] 2.2 将历史聚合键升级为复合键，确保同名任务按批次拆分
- [x] 2.3 为 `listResourceEvents` 增加资源 Kind 过滤
- [x] 2.4 为 `watchResourceEvents` 增加资源 Kind 过滤（防止流式串流）
- [x] 2.5 删除 `watchEvents` Header 明文打印
- [x] 2.6 在 `types.go` 对非三类 Reason 早返回

## 3. Operator 修复（D）
- [x] 3.1 在 `pkg/helper/event_reporter.go` 增加诊断事件限频能力
- [x] 3.2 在 `disasteroperation` 高频 `Recorder.Event*` 路径接入限频包装
- [x] 3.3 在 `cluster` 高频 `Recorder.Event*` 路径接入限频包装

## 4. Web 修复（E）
- [x] 4.1 在 `src/layout/index.vue` 增加全局 toast 窗口聚合
- [x] 4.2 聚合键采用 `taskName + reason + status`
- [x] 4.3 窗口内重复消息只刷新或累加，不新增弹窗

## 5. 测试与回归
- [x] 5.1 新增/更新 Server 单测，至少覆盖 B/C
- [x] 5.2 补充 A 的验证步骤与样例数据（同名任务不同 trace）
- [x] 5.3 补充 D 的验证步骤（diagnostic 事件限频前后对比）
- [x] 5.4 补充 E 的验证步骤（toast 数量前后对比）
- [x] 5.5 输出前后对比报告：事件数量、混并情况、串流情况
- [x] 5.6 修复验收完成后，回写 `openspec/specs/global-events/spec.md` 正式规范
