# Tasks: 自动清理 Helm 锁 (Automatic Helm Lock Cleanup)

## Development (开发)
- [x] **实现清理逻辑 (`internal/controller/velero_helpers.go`)**:
    - [x] 实现 `CleanupZombieHelmLocks` 函数。
    - [x] 入参：`ctx`, `cli` (目标集群客户端), `namespace` ("velero"), `releaseName` ("velero")。
    - [x] 逻辑：
        1. List Secrets with Labels: `owner=helm`, `name=velero`, `status` in [`pending-install`, `pending-upgrade`, `pending-rollback`]。
        2. 遍历结果，检查 Secret 的 `creationTimestamp`。
        3. 如果存在且时间 > `ZombieLockThreshold` (10m)，则视为僵尸锁。
        4. 执行 Delete。
    - [x] 在 `InstallVeleroInCluster` 中，在调用 `EnsureVeleroCRDs` 之前调用此清理函数。
    - [x] 清理失败不阻断主流程（尽力而为）。

## Testing (测试)
- [x] 编译通过 (`go build ./...`)
- [x] 增加单元测试用例 (`velero_helpers_test.go`):
    - [x] 场景：存在一个 `status=pending-upgrade` 且创建时间为 1 小时前的 Secret → 应被删除
    - [x] 场景：存在一个 `status=deployed` 的 Secret → **不**被删除
    - [x] 场景：存在一个 `status=pending-upgrade` 但创建时间为 10 秒前的 Secret → **不**被删除

## Verification (验证)
- [x] 编译通过
- [ ] 单元测试通过
- [ ] 在模拟环境手动创建一个 Label 为 `status=pending-upgrade` 的 Secret，触发 Reconcile，验证是否被自动删除
