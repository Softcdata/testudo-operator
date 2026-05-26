# 变更提案: 修复资源依赖检查与删除保护机制

## Why (为什么)

在 E2E 测试中发现了三个关键问题:

1. **缺少前置依赖检查**: Server 端创建 AppBackup 时,未验证目标 Cluster 和 StorageRepository 的就绪状态,导致在 Velero 未安装的集群上创建备份直接失败。

2. **删除保护过于严格**: Operator 删除 AppBackup 时,即使 Velero CRD 不存在(如 Velero 未安装),仍然尝试删除 Schedule 资源,导致 `no matches for velero.io/v1` 错误,阻塞了资源清理流程。

3. **E2E 测试流程缺陷**: 测试用例在添加集群后立即创建备份,未等待集群状态变为 Ready,导致测试不稳定。

这些问题会导致:
- 用户体验差: 创建资源后立即失败,且无法删除
- 测试不稳定: E2E 测试依赖时序,容易出现竞态条件
- 运维困难: 在 Velero 故障或卸载场景下,资源无法清理

## What Changes (变更内容)

### 1. Server 端 - 添加前置依赖验证 (disaster-server)

#### 1.1 封装验证函数 (可复用)
- **新增文件**: `internal/apis/common/validator.go`
- **函数**:
  - `ValidateClusterReady(ctx, client, clusterName) error` - 验证 Cluster 存在且状态为 Ready
  - `ValidateStorageRepositoryAvailable(ctx, client, repoName) error` - 验证 StorageRepository 存在且状态为 Available
- **返回**: 若验证失败,返回包含资源名称和当前状态的明确错误信息

#### 1.2 应用验证逻辑
- **位置**: `internal/apis/app_backup/v1/handler.go`
- **变更**: 在 `createAppBackup` 方法中,创建 AppBackup 之前:
  - 调用 `ValidateClusterReady()` 验证 Cluster
  - 调用 `ValidateStorageRepositoryAvailable()` 验证 StorageRepository
  - 若验证失败,返回 `400 Bad Request` 并使用验证函数返回的错误信息

### 2. Operator 端 - 优化删除保护逻辑 (disaster-operator)
- **位置**: `internal/controller/appbackup/appbackup_controller.go`
- **变更**: 在 `deleteExternalResources` 方法中:
  - 删除 Velero Schedule 前,先使用 `cli.List()` 检查 CRD 是否可访问
  - 若 `List` 返回 `meta.NoKindMatchError`,说明 Velero CRD 不存在,记录 Warning Event 并跳过删除
  - 若 CRD 存在,继续执行 `DeleteAllOf` 操作
  - 同样的逻辑应用于删除 Velero Backup (通过 DeleteBackupRequest)

### 3. E2E 测试 - 增加集群就绪等待 (disaster-e2e-test)
- **位置**: `test/e2e/scenarios/basic/basic_test.go`
- **变更**: 在 "步骤1: 初始化环境" 中:
  - 调用 `RegisterCluster` 后,增加 `Eventually` 轮询,等待 Cluster 状态变为 `Ready`
  - 超时时间设置为 3 分钟 (考虑 Velero 安装时间)
  - 轮询间隔 5 秒

## Impact (影响范围)

### 受影响的项目
- **disaster-server**: API 层增加验证逻辑
- **disaster-operator**: Controller 删除逻辑增强容错性
- **disaster-e2e-test**: 测试流程优化

### 受影响的规范
- `disaster-server/openspec/specs/app-backup/spec.md` - 新增前置验证需求
- `disaster-operator/openspec/specs/app-backup/spec.md` - 修改删除保护需求

### 受影响的代码
- `disaster-server/internal/apis/app_backup/v1/handler.go` - `createAppBackup` 方法
- `disaster-operator/internal/controller/appbackup/appbackup_controller.go` - `deleteExternalResources` 方法
- `disaster-e2e-test/test/e2e/scenarios/basic/basic_test.go` - 步骤1

### 破坏性变更
- **无破坏性变更**: 仅增强验证和容错,不改变现有 API 契约

## 技术细节

### Server 端验证逻辑伪代码
```go
// 1. 验证 Cluster
cluster, err := h.DisasterClient.DisasterV1().Clusters(ns).Get(ctx, req.Cluster, metav1.GetOptions{})
if err != nil {
    return 400, "Cluster not found"
}
if cluster.Status.Status != "Ready" {
    return 400, fmt.Sprintf("Cluster %s is not ready (current: %s)", req.Cluster, cluster.Status.Status)
}

// 2. 验证 StorageRepository
repo, err := h.DisasterClient.DisasterV1().StorageRepositories(ns).Get(ctx, req.StorageLocation, metav1.GetOptions{})
if err != nil {
    return 400, "StorageRepository not found"
}
if repo.Status.Status != "Available" {
    return 400, fmt.Sprintf("StorageRepository %s is not available (current: %s)", req.StorageLocation, repo.Status.Status)
}
```

### Operator 端容错逻辑伪代码
```go
// 检查 Velero CRD 是否存在
scheduleList := &velerov1.ScheduleList{}
err := cli.List(ctx, scheduleList, client.Limit(1))
if err != nil {
    if meta.IsNoMatchError(err) {
        logger.Info("Velero CRD not available, skipping Schedule deletion")
        r.Recorder.Event(ab, corev1.EventTypeWarning, "VeleroCRDNotFound", "Velero CRD not available, external resources may not be cleaned")
        return nil // 不阻塞删除流程
    }
    return err // 其他错误继续抛出
}

// CRD 存在,执行删除
err = cli.DeleteAllOf(ctx, &velerov1.Schedule{}, ...)
```

### E2E 测试等待逻辑
```go
It("步骤1: 初始化环境", func() {
    framework.RegisterCluster(f.APIClient, f.Config.Cluster)
    framework.RegisterStorage(f.APIClient, f.Config.Storage)
    
    By("等待集群状态变为 Ready")
    Eventually(func() string {
        cluster, err := f.APIClient.GetCluster(f.Config.Cluster.RegisterName)
        if err != nil {
            return ""
        }
        return cluster.Status.Status
    }, 3*time.Minute, 5*time.Second).Should(Equal("Ready"), "集群未能在规定时间内就绪")
})
```

## 风险与缓解

### 风险1: Server 端验证可能导致创建失败率上升
- **缓解**: 这是预期行为,通过明确的错误信息引导用户先修复依赖资源

### 风险2: Operator 跳过删除可能导致外部资源泄漏
- **缓解**: 
  - 仅在 CRD 不存在时跳过,这种情况下外部资源本身已不可访问
  - 记录 Warning Event,便于运维人员审计

### 风险3: E2E 测试等待时间过长
- **缓解**: 
  - 3 分钟超时是合理的(Velero 安装通常 1-2 分钟)
  - 可通过环境变量配置超时时间
