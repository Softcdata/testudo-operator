# 任务列表：控制器全量单元测试实现

## 1. 基础设施准备 (统筹)
- [x] 1.1 精简测试规划并接入 OpenSpec 系统
- [x] 1.2 提取 `cluster_controller_test.go` 中的 Mock 工具到 `internal/controller/common_test.go`
- [x] 1.3 确立各模块的子提案任务流

## 2. 子提案：AppBackup 测试实现 (Milestone)
- [x] 2.1 完成 `specs/app-backup/spec.md` 任务设计
- [x] 2.2 实现状态机迁移测试 (Pending -> Ready -> Failed)
- [x] 2.3 实现 Manual Action 测试 (Backup/Retry/Cancel)
- [x] 2.4 实现统计信息同步测试
- [x] 2.5 为 `AppBackup` Controller 补充单元测试，提高覆盖率 (Current: **78.9%**, Core handlers covered)

## 3. 子提案：AppRestore 测试实现 (Milestone)
- [x] 3.1 完成 `specs/app-restore/spec.md` 任务设计
- [x] 3.2 实现恢复生命周期测试
- [x] 3.3 实现 ConfigMap 插件 (SC/Image Mapping) 测试
- [x] 3.4 覆盖率检查 >80% ✅ (**80.3%** achieved)

## 4. 子提案：StorageRepository 与 Policy 测试实现
- [x] 4.1 实现存储库同步逻辑测试
- [x] 4.2 实现策略派生逻辑测试

## 5. 验收
- [x] 5.1 运行全量测试并归档 OpenSpec 变更 (**Overall Core: 79.5%**)

## Coverage Summary
| Module | Coverage | Status |
|--------|----------|--------|
| AppRestore | 80.3% | ✅ Target Met |
| AppBackup | 78.9% | ⚠️ Near Target |
| Combined Core | 79.5% | ✅ Good |
