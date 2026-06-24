# 设计：DataSync dataRestore Hook 与 Trafficless selector 兼容

## Context
Velero restore 流程中，init restore hook 由 RestoreItemAction 在 Pod 创建前处理，匹配对象是备份中恢复出来的原始 Pod。exec post-restore hook 在 Pod 创建后异步分组执行，匹配对象是已经经过 ResourceModifier、namespaceMapping 和 Velero restore label 注入后的 Pod。

DataSync 的 Trafficless 恢复会覆盖 Pod labels，只保留 `trafficless=true`，以避免 Service 导流和 workload controller selector 接管恢复出来的临时 Pod。因此不能通过保留原业务 labels 来修复 exec hook selector。

## Goals
- 让 DataSync dataRestore exec post hook 在 Trafficless 恢复下能真实执行。
- 保持 Trafficless 不导流、不被 controller 接管的隔离语义。
- 不改变用户提供的 hook 执行参数。
- 在同一个 Pod 同时匹配多个 hook resource 时，所有匹配的 exec hook 都应可执行。

## Non-Goals
- 不改变 Velero 原生 hook schema。
- 不新增 hook template 或参数渲染能力。
- 不保留业务 labels。

## Decisions
- 对包含 exec post hook 的 hook resource，生成独立系统 marker label key：`testudo.softcdata.com/data-restore-hook-<index>`，value 固定为 `"true"`。
- marker ResourceModifier 使用原 hook resource 的 namespace、resource、labelSelector 条件匹配备份中原始 Pod，并在 Trafficless labels 覆盖之后给恢复 Pod 添加 marker label。
- 对应 exec post hook resource 的 selector 重写为 marker selector。
- 含 init hook 和 exec hook 的同一个 hook resource 会拆成两条：
  - init-only resource 保持原 selector。
  - exec-only resource 使用 marker selector。
- marker 规则作为 system-protect rule 注入，确保在 restorePolicy 编译路径中排在 Trafficless labels 覆盖之后。
- 如果是 drill data restore，使用同一套 marker rewrite。marker ResourceModifier 的 namespace 条件匹配备份对象的原始命名空间；被重写的 exec hook resource namespace 条件按 `namespaceMapping` 映射到目标命名空间，因为 Velero exec hook 在 Pod 创建后按目标命名空间匹配。
- Trafficless 恢复 Pod 覆盖 `/spec/containers/0/command` 时同步覆盖 `/spec/containers/0/args=[]`，避免原业务 args 被新的 trafficless command 误消费导致容器崩溃，进而阻塞 exec post hook。

## Risks / Trade-offs
- AppRestore 中的 dataRestore hooks 不再与用户输入对象 byte-for-byte 相等；契约改为执行参数透明保留，系统可为兼容 Trafficless 执行语义重写 selector。
- marker label key 会出现在恢复后的临时 Pod labels 中，但不包含业务 selector，不会触发 Service 或 workload controller 接管。
- 如果用户 hook resource 显式排除了 `pods`，server 按既有契约应拒绝；operator 侧仍保守跳过 marker rewrite，避免扩大匹配范围。

## Validation
- 单元测试覆盖：
  - exec post hook selector 被重写为 marker selector。
  - marker ResourceModifier 位于 Trafficless labels 覆盖之后。
  - command/container/timeout/onError/waitForReady 保持不变。
  - init-only hook 保持原 selector，不注入 marker。
  - init+exec 混合 hook 被拆分。
  - 多个 hook resource 使用不同 marker key，可同时命中同一 Pod。
  - drill data restore 复用相同兼容规则，并按 namespaceMapping 改写 marker rule namespace 条件。
  - Trafficless Pod 同步清空原始 args，保证覆盖 command 后容器稳定运行。
