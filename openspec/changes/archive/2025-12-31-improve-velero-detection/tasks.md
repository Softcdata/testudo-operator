## 1. 实现改进 (disaster-operator)
- [x] 1.1 在 `internal/controller/cluster_controller.go` 中重构 `IsVeleroInstalled` 方法
- [x] 1.2 将检测逻辑从"检查 Deployment"改为"检查 Backup CRD 可用性"
- [x] 1.3 使用 `cli.List(&velerov1.BackupList{}, client.Limit(1))` 进行检测
- [x] 1.4 使用 `meta.IsNoMatchError(err)` 判断 CRD 是否存在
- [x] 1.5 添加必要的 import (`k8s.io/apimachinery/pkg/api/meta`)
- [x] 1.6 验证编译通过

## 2. 测试
- [x] 2.1 编写单元测试覆盖 CRD 存在/不存在场景
- [x] 2.2 手动测试: Velero 已安装场景
- [x] 2.3 手动测试: Velero 未安装场景
- [x] 2.4 手动测试: Velero CRD 存在但 Deployment 不存在场景

## 3. 文档更新
- [x] 3.1 更新 `openspec/specs/cluster/spec.md` (如果存在)
- [x] 3.2 在 `openspec/operator-best-practices.md` 中补充 CRD 检测最佳实践

## 4. 验证与发布
- [x] 4.1 运行 E2E 测试验证集群添加流程
- [x] 4.2 验证不会重复安装 Velero
- [x] 4.3 提交代码并创建 PR
- [x] 4.4 部署到测试环境验证
