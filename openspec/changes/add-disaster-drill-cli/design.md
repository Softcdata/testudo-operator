## Context

CLI 的调用方只提供子命令和容灾实例名称。创建请求需要参考实例详情；后续操作实际以 Drill 名称为路径参数，因此工具还需要完成实例到 Drill 的解析。服务端已提供按 `instanceName` 过滤且按创建时间倒序返回的列表接口。

## Goals / Non-Goals

- Goals: 用单文件 Bash CLI 覆盖登录、创建、确认、清理和状态查询；默认生成隔离演练命名空间；保持请求与前端/RunAPI 语义一致；脚本用户界面使用英文；错误可诊断。
- Non-Goals: 不修改 Server/Operator API；不支持容灾组；不新增重跑或删除子命令；不隐藏异步状态机。

## Decisions

- Decision: 未提供 `AUTH_TOKEN` 时，使用可覆盖的 `AUTH_USERNAME/AUTH_PASSWORD` 调用 `/login`，默认值为 `admin/123456`；请求体通过 stdin 传给 curl，且不输出凭据或 Token。
- Decision: 默认通过 `GET /instances/{name}` 获取实例详情，创建时显式传入实例真实 namespace、secondary cluster、modifier/bulk 文本规则和 dataRestore hooks。
- Decision: 默认把每个受保护 namespace 映射到本次调用唯一的目标 namespace，避免演练覆盖已有 standby namespace；可用 `DRILL_NAMESPACE_MAPPING='{}'` 显式关闭。
- Decision: 未设置 `DRILL_NAME` 时，动作命令通过 `GET /drills?instanceName=...&limit=1` 选择最新演练；设置后先校验该 Drill 确实属于输入实例。
- Decision: `execute` 只接受 `Ready`，`cleanup` 只在 `Completed` 时触发；已进入 `CleaningUp/CleanedUp` 时清理命令幂等返回当前状态。
- Alternatives considered: 本地状态文件会让跨机器和重复运行产生漂移；要求用户额外输入 Drill 名称不符合两参数契约；直接复用原 namespace 增加演练污染风险。

## Risks / Trade-offs

- 默认凭据只适用于当前本地环境；其他环境必须覆盖账号密码或显式传入 `AUTH_TOKEN`。
- 最近一次演练不一定是用户想操作的对象；通过 `DRILL_NAME` 提供确定性覆盖。
- API 创建成功只代表 CR 已创建；脚本保持异步语义，不把 201 误报为演练完成。
- 自动映射会生成新 namespace；清理必须等 Drill `Completed` 后显式执行。

## Migration Plan

新增独立脚本，不替换现有入口。出现问题时删除该脚本和对应 Harness 记录即可，不影响产品运行时。

## Open Questions

- 无。用户已明确限定实例演练和四个子命令。
