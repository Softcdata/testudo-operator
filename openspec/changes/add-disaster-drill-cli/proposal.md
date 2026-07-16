# Change: Add disaster drill CLI

## Why

容灾演练目前需要人工串联实例详情、创建、确认、状态查询和清理接口，容易遗漏实例级恢复规则或 Hook，也难以只凭实例名称稳定操作最近一次演练。需要一个可重复执行的 Bash 入口，把前端已验证的请求语义固化为 Harness 工具。

## What Changes

- 新增 `scripts/harness/disaster-drill.sh`，支持 `create|execute|cleanup|status <instance-name>`。
- 脚本未显式提供 Token 时使用 `admin/123456` 调用 `/login` 获取 access token，并保持源码、帮助和错误信息为英文。
- 创建前读取容灾实例详情，生成隔离的命名空间映射，并继承实例的 modifier/bulk 文本配置与 `veleroHooks.dataRestore`。
- 执行、清理和状态查询按实例名称定位最近一次演练，并允许通过 `DRILL_NAME` 精确指定。
- 增加无外部测试框架依赖的 Bash 自动化测试，并对本地服务执行 API/运行时验证。
- 增加中文使用手册，覆盖标准流程、配置、真实备份检查、状态门禁、失败处置与本地测试。

## Impact

- Affected specs: `harness-drill-cli`
- Affected code: `scripts/harness/disaster-drill.sh`, `scripts/harness/tests/disaster-drill-test.sh`
- Affected records: `docs/harness/disaster-drill-cli.md`, `docs/harness/*`, `TASK_CONTRACT.md`, `SESSION_STATE.md`
- Product API/Operator behavior: unchanged
