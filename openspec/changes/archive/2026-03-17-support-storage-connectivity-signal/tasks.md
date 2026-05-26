# Tasks: 支持跨集群恢复存储连通性触发信号

## 1. BDD 测试设计
- [x] 1.1 场景 A：存在双注解时，控制器必须使用 `bslName=<storage>-<sourceCluster>` 与 `prefix=<sourceCluster>`
- [x] 1.2 场景 B：缺失 `ensure-storage-source-cluster` 时，控制器必须回退到 `bslName=<storage>-<cluster.Name>` 与 `prefix=<cluster.Name>`
- [x] 1.3 场景 C：`StorageRepository` 不存在时，控制器必须清理触发注解并结束本次处理
- [x] 1.4 场景 D：`ApplyStorageRepository` 返回错误时，控制器必须保留触发注解并返回错误

## 2. 控制器实现
- [x] 2.1 在 `pkg/metadata/labels.go` 新增常量 `testudo.softcdata.com/ensure-storage-source-cluster`
- [x] 2.2 在 `cluster_controller.go` 解析双注解并计算 `bslName` 与 `prefix`
- [x] 2.3 调用 `r.BSL.ApplyStorageRepository(ctx, cli, sr, bslName, prefix)` 执行 BSL 对齐
- [x] 2.4 成功路径清理双注解并更新 `Cluster`
- [x] 2.5 `StorageRepository` 缺失路径清理双注解并记录 Warning 事件

## 3. 测试实现
- [x] 3.1 补充 `cluster_controller_test.go` 的双注解命名规则用例
- [x] 3.2 补充兼容回退规则用例
- [x] 3.3 补充注解消费与保留行为用例

## 4. 验证
- [x] 4.1 执行控制器单测，覆盖主路径与错误路径
- [x] 4.2 执行 `openspec validate support-storage-connectivity-signal --strict`
