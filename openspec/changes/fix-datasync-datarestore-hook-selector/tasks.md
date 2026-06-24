## 1. Proposal
- [x] 1.1 创建 OpenSpec proposal/design/tasks。
- [x] 1.2 添加 `disaster-instance`、`restore-builder`、`drill` delta spec。
- [x] 1.3 运行 `openspec validate fix-datasync-datarestore-hook-selector --strict`。

## 2. Implementation
- [x] 2.1 为 DataSync dataRestore exec hook 增加 Trafficless marker selector rewrite。
- [x] 2.2 将 marker ResourceModifier 作为 system-protect rule 注入，确保 labels 覆盖后执行。
- [x] 2.3 为 Drill data restore 复用同一 marker rewrite，并处理 namespaceMapping。
- [x] 2.4 保持 init hook 原始 selector 语义和 hook 执行参数不变。

## 3. Tests
- [x] 3.1 更新 DataSync dataRestore hook 透传测试，验证 selector rewrite 与 marker rule。
- [x] 3.2 增加 init-only、init+exec、多 hook resource 覆盖。
- [x] 3.3 增加 Drill dataRestore hook marker rewrite 覆盖。
- [x] 3.4 运行相关 Go 测试。

## 4. Verification
- [x] 4.1 运行 `go test ./internal/controller/datasync ./internal/controller/disasteroperation ./internal/controller/restore -count=1`。
- [x] 4.2 运行 `make harness-preflight`、`make harness-lint`、`make harness-ci`，若既有问题阻塞则记录原因。
- [x] 4.3 运行 `make test` 与 `make lint`，若既有问题阻塞则记录原因。
