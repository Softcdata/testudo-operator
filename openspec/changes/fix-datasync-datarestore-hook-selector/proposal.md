# Change: 修复 DataSync dataRestore Hook 在 Trafficless 恢复下的 selector 失配

## Why
前端 E2E 已证明 `DisasterInstance.spec.veleroHooks.dataRestore` 能透传到 `AppRestore` 和 Velero `Restore`，但 DataSync 数据恢复链路仍不会执行用户配置的 post-restore exec hook。根因是 Trafficless ResourceModifier 会将恢复 Pod 的业务 labels 覆盖为 `trafficless=true`，而 Velero exec restore hook 在 Pod 创建后使用最终 labels 匹配 selector，导致用户按原业务标签配置的 hook 无法命中。

## What Changes
- DataSync 构建 data restore AppRestore 时，为包含 exec post hook 的 restore hook resource 注入系统 marker label 规则。
- 将对应 exec post hook selector 重写为系统 marker selector，使其在 Trafficless labels 覆盖之后仍能命中恢复 Pod。
- 保留 Trafficless 隔离语义，不恢复业务 labels，不让恢复 Pod 被 Service 或 workload controller 接管。
- 保留 init restore hook 的原始 selector 语义，因为 init hook 在 Pod 创建前按备份对象原始 labels 匹配。
- 保留 hook command、container、timeout、onError、waitForReady 等执行参数。

## Impact
- Affected specs:
  - `disaster-instance`
  - `restore-builder`
  - `drill`
- Affected code:
  - `internal/controller/datasync/controller.go`
  - `internal/controller/disasteroperation/controller.go`
  - `internal/controller/datasync/*_test.go`
  - `internal/controller/disasteroperation/*_test.go`
