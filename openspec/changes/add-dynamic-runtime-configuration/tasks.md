# Tasks: 备份/恢复/容灾动态配置分层机制

## 1. Schema 与配置契约

- [x] 1.1 定义 `OperatorRuntimeConfig` API 类型与 CRD schema。
- [x] 1.2 为 runtime config 增加 `status.conditions`，表达 `Ready/Invalid` 与字段级错误。
- [x] 1.3 按 design 字段表定义 duration、minute、count 等字段类型、默认值和 controller 语义校验规则；CRD schema 只做结构/类型校验，不拦截范围错误。
- [x] 1.4 生成 deepcopy、CRD、RBAC，并完成 scheme 注册。
- [x] 1.5 编写配置分层文档，区分业务默认配置、operator runtime config、启动配置、Velero rollout 配置。

## 2. Runtime Config Provider

- [x] 2.1 实现默认 runtime config 构造器，保持现有代码默认行为。
- [x] 2.2 接入现有 `APPRESTORE_*` env 作为兼容启动默认值，覆盖 in-progress/unknown wait、stall grace、PVR pending、retryLimit/per-type retry limits 和 retryBackoff。
- [x] 2.3 实现 `OperatorRuntimeConfig/default` watch/cache。
- [x] 2.4 实现原子快照读取接口，供 controllers 在 reconcile 中读取。
- [x] 2.5 实现非法配置拒绝激活、保留最后有效快照、status/event 反馈。
- [x] 2.6 实现 `OperatorRuntimeConfig/default` 删除后回退启动默认值/代码默认值，并发出事件。
- [x] 2.7 修改默认值解析路径，确保 resource spec、runtime config、env/flag、代码默认值按优先级合并。

## 3. Controller 接入

- [x] 3.1 AppBackup 接入 backup runtime timeout 与 poll interval。
- [x] 3.2 AppRestore 接入 restore runtime timeout、stall grace、PVR pending、retry limit/backoff。
- [x] 3.2a AppRestore transient Get Restore 错误必须保持 Restoring 并按 retryBackoff requeue。
- [x] 3.2b AppRestore completed-progress stall 遇到活跃 PVR 不得触发 auto retry 或删除 Restore。
- [x] 3.2c AppRestore Velero restart 恢复动作必须在 startup transient、empty status 或仍有运行中 Velero 操作时跳过。
- [x] 3.3 DisasterOperation 接入默认 operation timeout、step requeue、默认 retry interval。
- [x] 3.4 DisasterInstance 接入 transition watchdog、初始化/稳态/失败轮询。
- [x] 3.5 DataSync/ResourceSync 接入 observe requeue、scheduler update timeout、history retention。
- [x] 3.5a DataSync/ResourceSync 将 `DisasterInstance.spec.operationTimeoutMinutes` 投递到托管 `AppBackup.spec.timeout`，并在 desired spec 对齐时比较 timeout。
- [x] 3.6 StorageRepository 接入校验/统计 requeue interval。
- [x] 3.7 Cluster 接入 reconcile interval、删除/卸载重试间隔、Velero install timeout、zombie lock threshold。

## 4. 前端动态传参与 Server/Web 边界文档

- [x] 4.1 在文档中定义业务默认配置域，供 server/web 后续实现。
- [x] 4.2 明确前端读取业务默认配置后写入 CRD `spec` 的字段映射。
- [x] 4.3 明确 operator 不消费业务默认配置，只消费最终 CRD `spec`。
- [x] 4.4 明确 `add-platform-settings` 只能作为底层存储，强类型业务默认配置 API 应由 server/web companion change 承接。
- [x] 4.5 标注 Velero 安装参数需要独立 rollout change，不作为 operator 热加载字段。

## 5. 测试与验证

- [x] 5.1 为 runtime config 默认值、覆盖、非法配置、删除回退添加单测。
- [x] 5.2 为 AppRestore/AppBackup/DisasterOperation 的热加载路径添加单测。
- [x] 5.2a 为 AppRestore transient API error、活跃 PVR、Velero restart skip 增加单测。
- [x] 5.2b 为 DataSync/ResourceSync AppBackup timeout 投递和既有 AppBackup timeout 对齐增加单测。
- [ ] 5.3 为 provider watch 行为添加 envtest 覆盖。
- [x] 5.4 运行 `make generate` 与 `make manifests`。
- [x] 5.5 运行受影响 package 测试。
- [x] 5.6 运行 `openspec validate add-dynamic-runtime-configuration --strict`。
- [ ] 5.7 运行 Code 阶段全量门禁 `make test` 与 `make lint`。
  - 当前状态：`make test` 已通过；`make lint` 已执行但因既有全仓 lint 债失败，新增变更范围 `bin/golangci-lint run --new-from-rev=HEAD ./...` 通过。
- [ ] 5.8 运行 `make harness-lint`、`make harness-preflight`、`make harness-ci`、`git diff --check`。
  - 当前状态：`make harness-preflight` 通过且仅有既有 warning；`git diff --check` 通过；`make harness-lint`/`make harness-ci` 因既有 decision 文档模板问题失败。

## 6. 发布与迁移

- [x] 6.1 提供默认 `OperatorRuntimeConfig` sample。
- [x] 6.2 更新部署说明，说明不配置 runtime config 时保持旧行为。
- [x] 6.3 标注 `APPRESTORE_*` env 的兼容状态和迁移建议。
- [x] 6.4 提供回滚说明：删除 runtime config 后回退启动默认值/代码默认值。
