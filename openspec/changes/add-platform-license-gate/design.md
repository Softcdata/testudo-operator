# Design: Platform License Gate

## 背景
`disaster-operator` 是容灾系统执行端，`Cluster` 是受管集群入口。商业授权的首期目标是约束“可添加的受管集群数量”：无有效 License 时最多 2 个，有效企业 License 时无限制。

由于项目采用 Apache-2.0 开源，授权机制必须被设计为官方发行版和企业权益控制，而不是开源许可证附加条款。技术实现需要覆盖 server/web 的用户体验，也需要在 operator 侧提供不可通过普通 Kubernetes 写入绕过的最终兜底。

## 关键决策

### D1. License 使用 JSON claims + Ed25519 离线签名
- License 文件后缀：`.lic`
- 文件内容：JSON
- 签名算法：Ed25519
- 产品内置公钥，发证工具持有私钥。
- 签名输入为去掉根对象 `signature` 字段后的 canonical JSON。
- canonical JSON 首期采用项目定义的 `disaster-platform-license-jcs-v1` profile：沿用 RFC 8785/JCS 的对象成员排序和字符串转义规则，移除根对象 `signature` 字段，并将 License schema 限制为 integer JSON number；如实现语言无法直接使用该 profile，必须提供同一组签发/验签测试向量证明字节完全一致。
- `signature` 编码为无 padding 的 base64url，内容为 Ed25519 64 字节签名。

原因：
- 私有化和离线客户可以不依赖公网激活。
- Ed25519 签名短、验签快、实现简单。
- 避免 HMAC 共享密钥被放入客户环境。
- 明确定义 canonicalization，避免内部发证工具、客户侧工具与 operator verifier 因 JSON 字段顺序、数字编码或签名编码差异而互不兼容。

### D2. 首期只支持 `k8s-v1` 部署指纹
- 指纹绑定管理集群部署，而不是单个物理机或 Node。
- 指纹信号：
  - `kube-system` namespace UID
  - 平台安装 namespace UID
  - API Server CA SHA256
  - `disaster-platform-install-id` Secret

原因：
- Operator Pod 会漂移，Node 会扩缩容；绑定单机硬件指纹会误伤客户。
- Kubernetes 管理集群是当前平台的真实运行边界。

### D3. License 存储在 Secret，状态输出到 ConfigMap
- 输入 Secret：`disaster-platform-license`
- 状态 ConfigMap：`disaster-platform-license-status`
- Operator 负责校验并写状态。
- Server/Web 可以读取状态用于展示、诊断和用户提示。
- Server、Cluster validating webhook 与 `ClusterReconciler` 的真实门禁不得信任状态 ConfigMap，必须基于签名 License Secret 和当前部署指纹重新计算 `Entitlement`，或调用等价的可信 verifier 路径。

原因：
- Secret 适合存放客户 License 文件。
- ConfigMap 适合对上层暴露脱敏、可缓存的授权状态。
- ConfigMap 位于客户集群内，可被具备足够 RBAC 的主体修改，因此只能作为派生输出，不能作为授权事实来源。
- 避免展示路径重复实现完整验签逻辑，但门禁路径必须使用可信 verifier。

### D4. 默认权益内置，License 失效时回退免费版
- 免费版默认 `maxClusters=2`。
- 任何 License 不存在或不可用状态均不得放大权益。
- 有效企业 License 使用 `maxClusters=-1` 表示无限制。

原因：
- 缺省状态应安全、可解释。
- 避免 License 读取失败时误开放企业权益。

### D5. License claims 必须先通过 schema 与产品校验
- `version` 必须为当前支持的 License schema 版本，首期仅支持 `1`。
- `product` 必须严格等于 `disaster-platform`。
- `keyId` 必须能映射到产品内置可信公钥。
- `limits.maxClusters` 必须存在，且必须是 JSON integer；合法值为 `-1` 或 `>= 0`。
- `fingerprintVersion` 必须为支持的指纹版本，首期仅支持 `k8s-v1`。
- `issuedAt`、`notBefore`、`expiresAt` 必须使用 UTC RFC3339 秒级格式，例如 `2026-05-07T00:00:00Z`，不得使用本地时区或小数秒。
- `notBefore` 不得晚于 `expiresAt`。

失败状态：
- JSON 解析失败、重复字段、字段缺失或字段类型错误：`Malformed`
- `version` 不支持：`UnsupportedVersion`
- `product` 不匹配：`ProductMismatch`
- `keyId` 未知：`UnknownKey`
- 签名不匹配：`InvalidSignature`
- 指纹不匹配：`FingerprintMismatch`
- 时间窗口未生效：`NotYetValid`
- 已过期：`Expired`

原因：
- 防止“签名有效但不是给本产品/本版本/本公钥集合签发”的 License 被误授予权益。
- 让 server、webhook、operator 和发证工具对负向场景有一致状态。

### D6. 门禁分三层
1. `disaster-server` 前置校验，提供清晰 API 错误。
2. `Cluster` validating webhook，拦截直接 Kubernetes 创建。
3. `ClusterReconciler` 兜底，阻止超限新增集群推进到 Ready。

原因：
- 单层限制都容易出现绕过或体验问题。
- 当前系统的实际写入路径既有 API，也可能有直接 CR 写入。

### D7. License 失效不得破坏存量容灾链路
- 不删除已有 `Cluster`。
- 不主动停止已有备份、恢复、故障切换。
- 不把已接受的存量 `Cluster` 降级为不可用。
- 限制范围聚焦新增超额 `Cluster` 和未来企业能力。

原因：
- 容灾产品的授权失效不应制造生产安全风险。
- 商业限制应避免破坏客户已有保护链路。

### D8. 接受态标记是强契约
- 每个被授权接受的 `Cluster` 必须写入接受态标记。
- 接受态标记是区分“已占用额度的存量对象”和“新建未接受对象”的事实来源。
- `ClusterReconciler` 必须在执行目标集群连接、Velero 安装或 Ready 推进前完成接受态判定与标记写入。

原因：
- Reconciler 处理对象时，CR 已经落库；如果直接用“所有未删除 Cluster 数量”作为创建前计数，会把免费版第 2 个 Cluster 误判为超限。
- 如果随意排除当前对象但没有接受态事实来源，又可能错误接受第 3 个直写对象。

## License Claims 草案

```json
{
  "version": 1,
  "licenseId": "LIC-20260507-0001",
  "product": "disaster-platform",
  "customer": {
    "name": "Acme Corp",
    "contact": "ops@example.com"
  },
  "edition": "enterprise",
  "fingerprintVersion": "k8s-v1",
  "fingerprint": "sha256:8c1f9b8a0f0a...",
  "issuedAt": "2026-05-07T00:00:00Z",
  "notBefore": "2026-05-07T00:00:00Z",
  "expiresAt": "2027-05-07T23:59:59Z",
  "limits": {
    "maxClusters": -1
  },
  "features": [
    "cluster.unlimited"
  ],
  "issuer": "your-company",
  "keyId": "ed25519-2026-01",
  "signature": "base64url-ed25519-signature"
}
```

## Entitlement 模型

Operator 内部不得在业务逻辑中直接散落读取 License JSON，而应统一转换为权益对象：

```go
type LicenseState string

const (
    LicenseStateFree                LicenseState = "Free"
    LicenseStateActive              LicenseState = "Active"
    LicenseStateExpired             LicenseState = "Expired"
    LicenseStateInvalidSignature    LicenseState = "InvalidSignature"
    LicenseStateFingerprintMismatch LicenseState = "FingerprintMismatch"
    LicenseStateNotYetValid         LicenseState = "NotYetValid"
    LicenseStateMalformed           LicenseState = "Malformed"
    LicenseStateUnsupportedVersion  LicenseState = "UnsupportedVersion"
    LicenseStateProductMismatch     LicenseState = "ProductMismatch"
    LicenseStateUnknownKey          LicenseState = "UnknownKey"
    LicenseStateUnknown             LicenseState = "Unknown"
)

type Entitlement struct {
    State       LicenseState
    Edition     string
    LicenseID   string
    Customer    string
    MaxClusters int
    ExpiresAt   *time.Time
    Features    map[string]bool
    Reason      string
    Message     string
}
```

策略函数：
- `ClusterLimit()`：无有效 License 返回 `2`；有效 License 返回 claims 中的 `maxClusters`。
- `CanCreateCluster(preCreateCount int)`：仅用于 server/webhook 创建前判断；`maxClusters < 0` 时允许，否则创建前数量达到上限时拒绝。
- `CanAcceptCluster(acceptedSiblingCount int)`：仅用于 Reconciler 落库后判断；`maxClusters < 0` 时允许，否则已接受的兄弟对象数量达到上限时拒绝当前未接受对象。
- `Allows(feature string)`：首期仅作为扩展点，核心门禁仍以 `maxClusters` 为准。

### 可信边界
- 可信输入：`disaster-platform-license` Secret 中的 License 文件在验签、时间窗口和指纹校验通过后形成的 `Entitlement`。
- 不可信输出：`disaster-platform-license-status` ConfigMap。
- 展示路径可以读取 ConfigMap。
- 门禁路径必须读取 License Secret 并重新计算 `Entitlement`，或调用同进程/可信服务提供的 verifier。
- 如果 ConfigMap 被用户篡改为 `state=Active` 或 `maxClusters=-1`，不得影响 server、webhook 或 Reconciler 对新增 `Cluster` 的真实判定。

## 指纹算法

### 输入信号
- `kube-system` namespace UID
- manager namespace UID，默认 `disaster-system`
- API Server CA SHA256
- `disaster-platform-install-id` Secret 中的 `install-id`

### API Server CA SHA256 规范
- CA 来源优先使用 in-cluster service account CA 文件 `/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`；客户侧工具使用 kubeconfig 时，读取当前 context cluster 的 `certificate-authority-data` 或 `certificate-authority`。
- 输入可以是 PEM bundle 或 DER。
- 实现必须解析所有 X.509 证书，取每个证书的原始 DER bytes。
- 对每个 DER 证书计算 SHA256 hex，按 hex 字符串升序排序。
- `apiServerCASHA256 = sha256(strings.Join(sortedDERCertSHA256Hex, "\n"))`，输出小写 hex。
- 如果 CA bundle 无法解析为 X.509 证书，指纹生成必须失败，不得退化为对原始文本直接 hash。

### 计算
```text
fingerprint = sha256(
  "disaster-platform:k8s-v1" + "\n" +
  kubeSystemUID + "\n" +
  platformNamespaceUID + "\n" +
  apiServerCASHA256 + "\n" +
  installID
)
```

输出格式：
```text
sha256:<hex>
```

### install-id 生命周期
- 首次安装若不存在，工具或 operator 生成 UUIDv4。
- 客户应备份该 Secret。
- 客户重装导致 install-id 丢失时，需要重新生成指纹并重新签发 License。

## 存储模型

### License Secret
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: disaster-platform-license
  namespace: disaster-system
  labels:
    testudo.softcdata.com/license: "true"
type: testudo.softcdata.com/license
data:
  license.lic: <base64>
```

### License Status ConfigMap
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: disaster-platform-license-status
  namespace: disaster-system
data:
  state: "Active"
  edition: "enterprise"
  licenseId: "LIC-20260507-0001"
  customer: "Acme Corp"
  expiresAt: "2027-05-07T23:59:59Z"
  maxClusters: "-1"
  clusterCount: "3"
  fingerprintMatched: "true"
  reason: ""
  message: "enterprise license is active"
  lastCheckedAt: "2026-05-07T03:00:00Z"
```

## Cluster 门禁规则

### 计数规则
创建前计数用于 server/webhook：
- 计入所有未删除的 `Cluster`，包括 `Pending`、`Ready`、`NotReady`。
- 不计入 `deletionTimestamp` 已设置的 `Cluster`。

Reconciler 接受计数用于 CR 已落库后的兜底：
- 若当前 `Cluster` 已带 `testudo.softcdata.com/license-accepted=true`，视为已接受存量对象，不重新消费额度。
- 若当前 `Cluster` 未被接受，则只统计“其他未删除且已接受”的 `Cluster`，即 accepted sibling count。
- 当 `acceptedSiblingCount >= limit` 且 limit 非无限时，拒绝当前未接受对象。
- 当 `acceptedSiblingCount < limit` 或 limit 为无限时，当前对象可以被接受，并必须先写入接受态标记再执行任何外部副作用。

原因：
- `NotReady` 仍是一个已添加的受管集群对象，server/webhook 不能允许用失败对象绕过额度。
- Reconciler 发生在对象落库后，必须使用接受态标记避免第 2 个对象被误判，也避免第 3 个直写对象被误接受。

### Create
- 当前数量 `< limit`：允许。
- 当前数量 `>= limit` 且 limit 非无限：拒绝。
- `limit=-1`：允许。

### Update
- 默认允许更新已有 `Cluster`，避免授权过期影响维护。
- 如果未来更新会改变计费维度，再新增约束。

### Delete
- 始终允许，授权状态不得阻止用户释放额度。

### Reconciler 兜底
Reconciler 必须按以下顺序执行：
1. 如果当前对象正在删除，跳过授权创建门禁。
2. 如果当前对象已带 `testudo.softcdata.com/license-accepted=true`，视为存量维护路径。
3. 如果当前对象未接受，先进入接受流程。
4. 接受流程必须串行化，避免两个未接受对象并发读取相同 accepted sibling count 后同时被接受。
5. 串行化方式可以是 controller 单 worker 保证、进程内锁，或 Kubernetes Lease；若 `MaxConcurrentReconciles > 1`，必须使用进程内锁或 Kubernetes Lease。
6. 在锁内重新读取当前对象和兄弟对象，计算 accepted sibling count。
7. 若超限，设置 `status.status=NotReady`、`status.reason=LicenseLimitExceeded`、清晰 `status.message`，发射 Warning Event，并停止后续外部副作用。
8. 若允许，必须 patch 接受态标记；patch 冲突时必须重试，不得继续执行外部副作用。
9. 接受态标记写入成功后，才允许连接目标集群、安装 Velero 或推进 `Ready`。

### 存量保护
被接受的 `Cluster` 必须记录注解：
```yaml
metadata:
  annotations:
    testudo.softcdata.com/license-accepted: "true"
    testudo.softcdata.com/license-accepted-at: "2026-05-07T00:00:00Z"
    testudo.softcdata.com/license-id: "LIC-20260507-0001"
```

License 过期后，该注解用于区分“已接受存量对象”和“新建超限对象”，避免把存量容灾链路误降级。

### 升级迁移
首次启用 license gate 的版本升级必须 grandfather 已存在的未删除 `Cluster`：
- Operator 启动时创建或读取 `disaster-platform-license-gate-state` ConfigMap，记录 `enabledAt`。
- `enabledAt` 创建前已经存在且未删除的 `Cluster`，若缺少接受态标记，必须回填：
  - `testudo.softcdata.com/license-accepted=true`
  - `testudo.softcdata.com/license-accepted-at=<migration time>`
  - `testudo.softcdata.com/license-id=grandfathered`
  - `testudo.softcdata.com/license-accepted-reason=pre-license-gate-upgrade`
- 回填完成前不得因 License 额度不足降级这些存量 `Cluster`。
- `enabledAt` 之后创建的未接受 `Cluster` 必须进入正常额度门禁，不得被 grandfather。
- 管理员可以选择在升级前安装企业 License，但系统不得要求已有 3 个集群的现场必须先安装 License 才能避免存量链路被破坏。

## 跨仓库 verifier 契约

### 所有权与复用
- `disaster-operator` 维护 `platform-license` OpenSpec 与 Go reference verifier。
- `disaster-server` 添加集群门禁必须复用同一 verifier 语义；首选方式是依赖共享 Go module/package。
- 如果 server 不能直接复用同一包，必须引入同一组 conformance test vectors，覆盖 valid、product mismatch、unknown key、unsupported version、malformed limits、expired、not yet valid、fingerprint mismatch、invalid signature、canonical payload、duplicate fields、non-integer number、environment invalid 和 ConfigMap tampering。
- Web 不实现 verifier，只展示 server/operator 输出。

### Namespace 与 RBAC
- License namespace 由 chart 统一配置；默认 `disaster-system`。
- Operator 通过 manager namespace 或显式 `--license-namespace` 获取。
- Server 通过配置项读取同一 namespace，不得硬编码不同默认值。
- Server 添加集群门禁需要 `get` License Secret、`list` Cluster；License 上传接口才需要 `create/update/patch` License Secret。
- Server 不需要、也不得依赖更新 License status ConfigMap 完成门禁。

### 错误码与兼容
- Server、webhook、Reconciler 必须使用一致的错误原因：`LicenseLimitExceeded`、`LicenseInvalid`、`LicenseExpired`、`LicenseFingerprintMismatch`、`LicenseUnsupportedVersion`、`LicenseUnknownKey`、`LicenseProductMismatch`。
- License schema version 不支持时必须失败关闭到免费版权益，不得尝试按旧字段猜测。

## Server / Web 行为

### Server
- 暴露 `GET /license/status`。
- 暴露 License 上传或安装接口，写入 License Secret。
- 添加集群 API 在创建 `Cluster` 前必须通过可信 verifier 计算 `Entitlement` 并结合 Cluster 计数校验。
- `GET /license/status` 可以返回 ConfigMap 中的展示状态；添加集群门禁不得把 ConfigMap 作为放行依据。
- 超限时返回稳定错误码与中文提示。

### Web
- 顶部或系统设置页展示授权状态。
- 添加第 3 个集群时展示升级提示。
- 支持上传 `.lic`。
- 到期前展示提醒，到期后展示限制说明。

## 发证工具

### 客户侧
```bash
disasterctl license fingerprint \
  --namespace disaster-system \
  --out fingerprint-request.json
```

### 内部发证
```bash
disaster-license issue \
  --request fingerprint-request.json \
  --customer "Acme Corp" \
  --contact "ops@example.com" \
  --edition enterprise \
  --max-clusters unlimited \
  --not-before 2026-05-07T00:00:00Z \
  --expires 2027-05-07T23:59:59Z \
  --key-id ed25519-2026-01 \
  --private-key issuer-private.pem \
  --out disaster-platform.lic
```

### 客户安装与查看
```bash
disasterctl license install \
  --namespace disaster-system \
  --file disaster-platform.lic

disasterctl license status \
  --namespace disaster-system
```

## 安全要求
- 私钥不得进入仓库、镜像、chart、测试夹具或 CI 日志。
- 产品镜像只内置公钥。
- 支持 `keyId` 到公钥的映射，用于公钥轮换。
- License 状态 ConfigMap 不得暴露签名密钥或原始指纹信号。
- License 状态 ConfigMap 不得作为授权门禁的可信来源。
- 普通用户应只具备读取 License 状态 ConfigMap 的权限，不应具备更新、patch 或删除权限。
- 普通用户不得读取 License Secret。
- 指纹请求不得包含 kubeconfig、token、CA 明文。

## 分期

### 本 change 完成边界
`add-platform-license-gate` 是完整跨仓库交付，不是仅 Operator MVP。归档前必须完成以下能力：
1. Operator License verifier、`k8s-v1` 指纹、License Secret 读取、License status ConfigMap 输出。
2. ClusterReconciler accepted sibling count 兜底、接受态标记、升级 grandfather 和存量保护。
3. Cluster validating webhook CREATE 门禁，纳入生产安装路径，并通过真实 Helm 安装/升级验证 webhook 资源存在且可在 admission 阶段拒绝超限 `Cluster` 创建。
4. Server License 状态 API、License 上传/安装接口、添加集群前置门禁、统一 verifier 语义和稳定错误码。
5. Web 授权状态展示、License 上传入口和添加第 3 个集群时的明确提示。
6. Chart / deploy 资源，包括 License Secret 安装说明、status ConfigMap/RBAC、webhook、统一 License namespace。
7. 客户侧 fingerprint/install/status 工具和内部 keygen/issue/verify 工具。
8. 跨 operator/server/tooling 的 conformance test vectors。
9. Operator、Server、Web、Chart 与真实环境 runbook 验证任务全部完成；仅完成 Chart render 不等价于运行时 webhook 闭环。

### 可分 PR 顺序
为降低风险，本 change 可以拆成多个短 PR，但这些 PR 都属于同一个 OpenSpec change，不能在只完成前半部分时归档：
1. PR1：License verifier、canonical JSON、claims 校验、指纹和工具测试向量。
2. PR2：Operator License Secret、状态 ConfigMap、Reconciler accepted 标记、grandfather 与兜底门禁。
3. PR3：Cluster validating webhook 与 chart 安装路径。
4. PR4：Server License API、上传接口、添加集群前置门禁与错误码。
5. PR5：Web 授权展示、上传和超限提示。
6. PR6：真实环境 runbook 与跨仓库验收收敛。

### 明确不在本 change 内
1. 在线激活、在线续费或在线撤销。
2. 除 `Cluster` 数量外的复杂功能 entitlement。
3. 将开源协议切换为 MPL/GPL/AGPL 或 source-available 协议。

## 备选方案

### 方案 A：只在 Web 限制第 3 个集群
- 放弃原因：API 与 kubectl 可绕过。

### 方案 B：只在 Server 限制
- 放弃原因：用户可直接创建 `Cluster` CR。

### 方案 C：绑定单机硬件指纹
- 放弃原因：Operator 运行在 Kubernetes 中，Pod 和 Node 生命周期不稳定。

### 方案 D：使用 HMAC 签名
- 放弃原因：验证端需要内置共享密钥，客户环境可能被逆向获得签发能力。
