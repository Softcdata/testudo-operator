# Change: Trace User Identity via Annotation

## Why
目前系统事件中缺乏统一的操作人记录机制，导致审计困难。虽然 `AppBackup` 等部分资源实现了用户字段读取，但未形成全局规范，也未覆盖 `Cluster`, `Storage` 等核心资源。

## What Changes
- **规范层面**: 在 `global-events` 中强制要求所有 Controller 通过 `testudo.softcdata.com/user` 注解识别操作人。
- **实现层面**: 
    - 修改 `pkg/helper/event_reporter.go` (如果需要) 或各 Controller 逻辑，确保从 Annotation 读取用户。
    - 确保 Cluster, Storage, DisasterPolicy 等 Controller 遵循此规范。

## Impact
- **Affected Specs**: `global-events`
- **Affected Code**: `internal/controller/*`
