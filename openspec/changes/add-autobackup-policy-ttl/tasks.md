# Tasks: Velero 自动备份策略 TTL

## 1. Proposal

- [x] 1.1 评审 `proposal.md`、`design.md` 与 delta specs。
- [x] 1.2 确认 TTL 输入格式采用 Kubernetes duration 字符串，例如 `24h`、`720h`。
- [x] 1.3 确认非 AutoBackup 策略携带 TTL 的处理方式为 Server 拒绝、Operator 忽略。

## 2. Operator

- [x] 2.1 在 `DisasterPolicySpec` 新增 `ttl` 字段，类型为 `*metav1.Duration`。
- [x] 2.2 更新 `DisasterPolicy` CRD schema 与 sample。
- [x] 2.3 在 `DisasterPolicyReconciler` 中增加 TTL 校验，非法时设置稳定错误 `InvalidTTL`。
- [x] 2.4 在 `AppBackup` 策略继承逻辑中，当引用 `AutoBackup` 策略且策略 TTL 非空时写入 `appBackup.spec.template.ttl`。
- [x] 2.5 确认 Velero `Schedule.spec.template.ttl` 在创建和更新路径都与 AppBackup 模板一致。
- [x] 2.6 补充单元测试：AutoBackup 策略 TTL 透传到 AppBackup 模板。
- [x] 2.7 补充单元测试：Velero Schedule 创建/更新时携带 TTL。
- [x] 2.8 补充单元测试：非法 TTL 进入 `InvalidTTL` 状态。
- [x] 2.9 补充单元测试：DataSync/ResourceSync 策略 TTL 不参与调度。

## 3. Server

- [x] 3.1 在 `DisasterPolicySpecDTO`、创建请求、更新请求中增加 `ttl`。
- [x] 3.2 创建策略时将 `ttl` 写入 CRD。
- [x] 3.3 更新策略时支持 TTL 部分更新，未传不清空，显式清空有确定协议。
- [x] 3.4 对非 `AutoBackup` 策略携带 TTL 做提交期拒绝。
- [x] 3.5 策略列表与详情响应回显 `spec.ttl`。
- [x] 3.6 AppBackup 详情响应回显实际生效 `spec.ttl`。
- [x] 3.7 更新 Apipost / API 文档与响应示例。
- [x] 3.8 补充 server 单测：创建、更新、详情、列表、非法类型携带 TTL。

## 4. Web

- [x] 4.1 策略管理表单在 `AutoBackup` 类型下显示 TTL 输入。
- [x] 4.2 策略管理表单提交 `ttl`，编辑时回填 `ttl`。
- [x] 4.3 策略列表或详情展示 TTL。
- [x] 4.4 自动备份创建向导选择策略后展示策略 TTL，避免与手动 TTL 输入产生覆盖歧义。
- [x] 4.5 自动备份详情页确认显示 `detail.spec.ttl`。

## 5. Chart / Release

- [x] 5.1 同步 `DisasterPolicy` CRD 到 `disaster-system-chart`。
- [x] 5.2 标记 CRD 向后兼容，无存量数据迁移。

## 6. Verification

- [x] 6.1 `openspec validate add-autobackup-policy-ttl --strict`。
- [x] 6.2 Operator 定向测试：DisasterPolicy Ginkgo、AppBackup TTL 继承、Velero Schedule 模板 TTL。
- [x] 6.3 Server 定向测试：`go test ./internal/apis/disaster_policy/v1 -count=1`。
- [ ] 6.4 Web 类型检查与相关页面 smoke test。
- [ ] 6.5 E2E：创建带 TTL 的 AutoBackup 策略，创建引用该策略的 AppBackup，验证 Velero Schedule 模板 TTL、AppBackup 详情 TTL、历史过期时间。
