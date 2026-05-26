# Tasks: 添加集群时增加 Velero/CRD 版本适配门禁

## 1. OpenSpec

- [x] 1.1 创建提案与设计文档，明确版本门禁、CRD 门禁与报错语义。
- [x] 1.2 为 `cluster` 能力补充增量规范（添加集群失败场景）。

## 2. 测试先行（BDD 场景设计）

- [x] 2.1 场景 A：Velero 版本不兼容时，`Cluster` 进入 `NotReady` 且 `reason=VeleroVersionIncompatible`。
- [x] 2.2 场景 B：关键 CRD 缺失/版本不兼容时，`Cluster` 进入 `NotReady` 且 `reason=VeleroCRDVersionIncompatible`。
- [x] 2.3 场景 C：CRD 检测失败（权限/连接）时，`reason=VeleroCRDCheckFailed`。
- [x] 2.4 场景 D：版本与 CRD 均兼容时，`Cluster` 进入 `Ready`。
- [x] 2.5 场景 E：添加集群失败时发射 `TaskFinished(Failed)` 结构化事件。

## 3. 测试实现

- [x] 3.1 在 `internal/controller/cluster_controller_test.go` 增加上述 BDD 用例。
- [x] 3.2 为版本解析与范围比较补充单元测试（含 `v` 前缀与非法字符串）。
- [x] 3.3 为 CRD 兼容性校验函数补充单元测试（served/storage 组合）。

## 4. 实现

- [x] 4.1 新增 Velero 兼容策略定义（支持版本范围、关键 CRD 列表、期望 CRD 版本）。
- [x] 4.2 在 `ClusterReconciler` 增加 `checkVeleroCompatibility` 入口并接入创建主链路。
- [x] 4.3 实现 Velero 版本范围校验，失败时设置 `reason/message` 并发射告警事件。
- [x] 4.4 实现 CRD 版本校验，失败时设置 `reason/message` 并发射告警事件。
- [x] 4.5 添加“创建集群失败”结构化结束事件（兼容性失败分支）。

## 5. 验证

- [x] 5.1 运行 `go test ./internal/controller -run Cluster -v`（或等价命令）验证新增测试。
- [x] 5.2 运行 `openspec validate add-cluster-velero-compatibility-gate --strict`。
- [ ] 5.3 与 `disaster-server` / `cluster-disaster-web` 对齐 reason 映射与展示文案。
