# 任务清单：添加 DisasterPolicy CRD

## 背景
引入 `DisasterPolicy` CRD 以统一管理灾备策略。

## 任务列表

### 1. 定义 CRD
- [ ] 在 `pkg/apis/disaster/v1/` 中创建 `disasterpolicy_types.go`。
  - 定义 `PolicyType` 和 `PolicyState` 枚举。
  - 定义 `DisasterPolicySpec` 和 `DisasterPolicyStatus`。
  - 添加必要的 Kubebuilder 标记（验证、默认值、打印列）。
- [ ] 运行 `make manifests` 和 `make generate` 生成代码和 YAML。

### 2. 实现控制器
- [ ] 使用 `kubebuilder create api` 或手动创建 `internal/controller/disasterpolicy_controller.go`。
- [ ] 实现 Reconcile 逻辑：
  - 获取 `DisasterPolicy` 实例。
  - 验证 `Schedule` Cron 表达式格式。
  - 实现标签同步逻辑：
    - `testudo.softcdata.com/policy-type`
    - `testudo.softcdata.com/policy-name`
    - `testudo.softcdata.com/policy-state`
  - 更新 Status（如需）。

### 3. 测试
- [ ] 编写 CRD 验证测试。
- [ ] 编写控制器单元测试，验证标签同步功能。

### 4. 文档
- [ ] 更新 API 文档。
- [ ] 提供 `DisasterPolicy` 的示例 YAML 文件。

## 优先级
1. 定义 CRD。
2. 实现控制器（标签同步）。
3. 测试与文档。
