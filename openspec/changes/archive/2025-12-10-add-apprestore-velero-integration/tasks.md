# 任务清单：添加 AppRestore 和 Velero 集成

## 背景
为了实现 AppRestore 和 Velero Restore 资源之间的一对一管理关系，确保应用恢复操作的准确性和可追溯性，需要完成以下任务。

## 任务列表

### 1. 定义 CRD
- [x] 在 `pkg/apis/apprestore/v1/` 中定义 `AppRestore` CRD。
  - 包括 `Spec` 和 `Status` 字段。
  - 确保字段设计支持一对一关系。
  - 添加必要的注解和标签支持。

### 2. 实现控制器
- [x] 在 `internal/controller/` 中实现 `AppRestoreReconciler`。
  - 根据 `AppRestore` 的状态选择对应的处理器。
  - 确保状态机逻辑正确处理 `Pending`、`Restoring` 等状态。
  - 集成标签同步逻辑。

### 3. 状态处理器
- [x] 实现 `PendingHandler`。
  - 验证集群和存储库。
  - 确保所有检查通过后，更新状态为 `Restoring`。
- [x] 实现 `RestoringHandler`。
  - 创建 Velero Restore 资源。
  - 监控 Velero Restore 的状态并更新 `AppRestore`。
- [x] 实现 `RetryRestore`。
  - 删除现有的 Velero Restore 并重新创建。
  - 确保重试逻辑不依赖历史记录。

### 4. 测试
- [x] 为 CRD 定义编写单元测试。
- [x] 为控制器逻辑编写单元测试。
- [x] 为每个状态处理器编写单元测试。
  - 验证状态转换逻辑。
  - 验证错误处理和事件记录。

### 5. 文档
- [x] 更新用户文档，描述 `AppRestore` 的使用方法。
  - 包括 CRD 字段说明。
  - 提供示例 YAML。
- [x] 添加开发文档，描述控制器和状态处理器的实现细节。

### 6. 验证
- [x] 部署到测试环境，验证 `AppRestore` 的功能。
  - 测试一对一关系是否正确。
  - 验证重试逻辑是否按预期工作。

### 7. 代码审查
- [x] 提交代码并进行审查。
  - 确保代码符合项目的开发标准。
  - 修复审查中发现的问题。

### 8. 发布
- [x] 合并代码并发布新版本。
  - 更新版本号。
  - 发布变更日志。

---

## 优先级
1. 定义 CRD。
2. 实现控制器和状态处理器。
3. 编写测试。
4. 部署验证。
5. 发布。