## 1. Server 端实现 (disaster-server)
- [x] 1.1 创建 `internal/common/validator.go` 文件
- [x] 1.2 实现 `ValidateClusterReady` 函数 (验证 Cluster 存在且 Status.Status == "Ready")
- [x] 1.3 实现 `ValidateStorageRepositoryAvailable` 函数 (验证 StorageRepository 存在且 Status.Status == "Available")
- [x] 1.4 在 `internal/apis/app_backup/v1/handler.go` 的 `createAppBackup` 方法中调用验证函数
- [x] 1.5 为验证函数编写单元测试 (`internal/common/validator_test.go`):
  - [x] 1.5.1 Cluster 不存在
  - [x] 1.5.2 Cluster 状态非 Ready
  - [x] 1.5.3 StorageRepository 不存在
  - [x] 1.5.4 StorageRepository 状态非 Available
  - [x] 1.5.5 所有依赖就绪,验证通过
- [x] 1.6 为 `createAppBackup` 集成测试验证端到端行为

## 2. Operator 端实现 (disaster-operator)
- [x] 2.1 在 `internal/controller/appbackup/appbackup_controller.go` 中重构 `deleteExternalResources` 方法
- [x] 2.2 添加 Velero Schedule CRD 可用性检查 (使用 `List` + `NoMatchError` 判断)
- [x] 2.3 添加 Velero Backup CRD 可用性检查
- [x] 2.4 在 CRD 不可用时记录 Warning Event 并跳过删除
- [x] 2.5 编写 Ginkgo 测试用例覆盖删除场景:
  - [x] 2.5.1 Velero CRD 存在,正常删除
  - [x] 2.5.2 Velero CRD 不存在,跳过删除且不阻塞 Finalizer 移除
  - [x] 2.5.3 Cluster 不存在,跳过删除 (现有逻辑已支持)

## 3. E2E 测试优化 (disaster-e2e-test)
- [x] 3.1 在 `test/e2e/scenarios/basic/basic_test.go` 的步骤1中增加集群就绪等待
- [x] 3.2 在 `test/e2e/framework/setup_helper.go` 中封装 `WaitForClusterReady` 辅助函数
- [x] 3.3 配置合理的超时时间 (默认 3 分钟) 和轮询间隔 (5 秒)
- [x] 3.4 在 `client/server_client.go` 中添加 `GetCluster` 方法
- [x] 3.5 在 `client/types.go` 中添加 `ClusterDTO` 类型定义
- [x] 3.6 验证测试在 Velero 未安装场景下的行为 (应在步骤1失败,而非步骤3)

## 4. 文档更新
- [x] 4.1 更新 `disaster-server/openspec/specs/app-backup/spec.md` 增加前置验证需求
- [x] 4.2 更新 `disaster-operator/openspec/specs/app-backup/spec.md` 修改删除保护需求
- [x] 4.3 在 `disaster-operator/openspec/operator-best-practices.md` 中补充 CRD 可用性检查最佳实践

## 5. 验证与测试
- [x] 5.1 手动测试: 在 Cluster 未就绪时尝试创建 AppBackup,验证错误信息
- [x] 5.2 手动测试: 卸载 Velero 后删除 AppBackup,验证能正常移除 Finalizer
- [x] 5.3 运行完整 E2E 测试套件,确保 basic 场景通过
- [x] 5.4 运行 Operator 单元测试,确保覆盖率不低于 80%
- [x] 5.5 运行 Server 单元测试,确保新增验证逻辑有测试覆盖

## 6. 部署与发布
- [x] 6.1 提交代码到 feature 分支
- [x] 6.2 创建 Pull Request 并关联此提案
- [x] 6.3 通过 Code Review
- [x] 6.4 合并到主分支
- [x] 6.5 部署到测试环境验证
