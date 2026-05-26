## ADDED Requirements

### Requirement: 系统必须提供默认免费版权益
系统必须 (MUST) 在未安装有效 License 时使用免费版权益，并将免费版 `Cluster` 数量上限设置为 2。

#### Scenario: 未安装 License 时使用免费版权益
- **Given** 管理集群中不存在 `disaster-platform-license` Secret
- **When** 系统计算当前平台权益
- **Then** License 状态必须为 `Free`
- **And** `maxClusters` 必须为 `2`

#### Scenario: License 无效时回退免费版权益
- **Given** 管理集群中存在 License Secret
- **And** License 文件签名无效、格式错误、指纹不匹配、尚未生效或已过期
- **When** 系统计算当前平台权益
- **Then** 系统不得授予企业版权益
- **And** `maxClusters` 必须回退为 `2`

### Requirement: 系统必须支持离线签名 License
系统必须 (MUST) 支持使用 Ed25519 签名的 JSON License 文件，并在客户环境中仅依赖内置公钥完成离线验签。

#### Scenario: 有效签名 License 被接受
- **Given** License 文件包含完整 claims
- **And** `signature` 是使用受信任 `keyId` 对 canonical JSON payload 生成的 Ed25519 签名
- **When** Operator 校验 License
- **Then** 验签必须成功
- **And** 系统必须继续校验产品名、时间窗口和部署指纹

#### Scenario: 被篡改 License 被拒绝
- **Given** License 文件内容在签发后被修改
- **When** Operator 校验 License
- **Then** 验签必须失败
- **And** License 状态必须为 `InvalidSignature`
- **And** 系统必须回退免费版权益

### Requirement: License 签名输入必须使用唯一 canonical JSON 规则
系统必须 (MUST) 使用唯一的 canonical JSON 字节序列进行签发和验签，避免不同工具实现产生不一致签名。

#### Scenario: 使用受约束 JCS canonical JSON profile 验签
- **Given** License JSON 根对象包含 `signature` 字段
- **When** 系统准备验签 payload
- **Then** 必须移除根对象的 `signature` 字段
- **And** 必须使用项目定义的 `disaster-platform-license-jcs-v1` profile 生成 UTF-8 payload bytes；该 profile 采用 RFC 8785/JCS 的对象成员排序和字符串转义规则，并将 License schema 限制为 integer JSON number
- **And** `signature` 必须按无 padding base64url 解码为 64 字节 Ed25519 签名
- **And** `keyId` 字段必须包含在被签名 payload 中
- **And** 重复对象字段和非 integer JSON number 必须被拒绝

#### Scenario: 非 canonical 实现必须通过测试向量
- **Given** 某个工具或组件无法直接使用 RFC 8785 JCS 库
- **When** 该组件实现自己的 canonical encoder
- **Then** 它必须通过与发证工具、operator verifier 相同的 conformance test vectors
- **And** 相同 License claims 必须产生完全相同的 payload bytes

### Requirement: License claims 必须通过产品、版本、公钥和额度 schema 校验
系统必须 (MUST) 在授予企业权益前校验 License claims 的产品名、schema 版本、`keyId`、时间格式、指纹版本和额度字段。

#### Scenario: product 不匹配时拒绝 License
- **Given** License claims 中 `product` 不等于 `disaster-platform`
- **When** 系统校验 License
- **Then** License 状态必须为 `ProductMismatch`
- **And** 系统必须回退免费版权益

#### Scenario: version 不支持时拒绝 License
- **Given** License claims 中 `version` 不在当前支持列表内
- **When** 系统校验 License
- **Then** License 状态必须为 `UnsupportedVersion`
- **And** 系统必须回退免费版权益

#### Scenario: keyId 未知时拒绝 License
- **Given** License claims 中 `keyId` 无法映射到产品内置可信公钥
- **When** 系统校验 License
- **Then** License 状态必须为 `UnknownKey`
- **And** 系统必须回退免费版权益

#### Scenario: maxClusters 缺失或非法时拒绝 License
- **Given** License claims 缺少 `limits.maxClusters`，或该字段不是 JSON integer，或该字段小于 `-1`
- **When** 系统校验 License
- **Then** License 状态必须为 `Malformed`
- **And** 系统必须回退免费版权益

#### Scenario: 时间格式非法时拒绝 License
- **Given** License claims 中 `issuedAt`、`notBefore` 或 `expiresAt` 不是 UTC RFC3339 秒级格式
- **Or** `notBefore` 晚于 `expiresAt`
- **When** 系统校验 License
- **Then** License 状态必须为 `Malformed`
- **And** 系统必须回退免费版权益

### Requirement: License 必须绑定部署指纹
License 必须 (MUST) 绑定客户部署环境指纹，首期指纹版本为 `k8s-v1`。

#### Scenario: 指纹匹配时允许继续权益计算
- **Given** License claims 中的 `fingerprintVersion` 为 `k8s-v1`
- **And** License claims 中的 `fingerprint` 与当前管理集群部署指纹一致
- **When** Operator 校验 License
- **Then** 指纹校验必须通过
- **And** 系统必须继续校验时间有效期

#### Scenario: 指纹不匹配时回退免费版
- **Given** License claims 中的部署指纹与当前管理集群部署指纹不一致
- **When** Operator 校验 License
- **Then** License 状态必须为 `FingerprintMismatch`
- **And** 系统必须回退免费版权益

### Requirement: k8s-v1 指纹必须使用确定性 CA hash
`k8s-v1` 指纹必须 (MUST) 使用确定性 API Server CA hash 规则，保证客户侧指纹工具、发证侧验收工具和 operator verifier 计算一致。

#### Scenario: 从 CA bundle 计算 API Server CA hash
- **Given** 指纹工具读取到 PEM bundle 或 DER 格式的 API Server CA
- **When** 工具计算 `apiServerCASHA256`
- **Then** 必须解析所有 X.509 证书并取每个证书的原始 DER bytes
- **And** 必须计算每个 DER 证书的 SHA256 hex
- **And** 必须按 hex 字符串升序排序
- **And** 必须对 `strings.Join(sortedDERCertSHA256Hex, "\n")` 的 UTF-8 bytes 计算 SHA256
- **And** 输出必须为小写 hex

#### Scenario: CA bundle 无法解析时失败
- **Given** 指纹工具读取到的 CA bundle 无法解析为 X.509 证书
- **When** 工具生成 `k8s-v1` 指纹
- **Then** 指纹生成必须失败
- **And** 工具不得退化为对原始 CA 文本直接 hash

#### Scenario: License 内容错误优先于部署指纹错误
- **Given** 管理集群中存在 License Secret
- **And** License 文件 schema malformed、`keyId` 未知或签名无效
- **And** 当前组件无法读取 API Server CA 或无法计算部署指纹
- **When** 系统计算当前平台权益
- **Then** 系统必须优先返回 License 内容对应的状态和 reason
- **And** 不得用部署指纹错误掩盖 License 内容错误

#### Scenario: 部署指纹计算失败返回环境错误
- **Given** License 文件内容、key、签名和时间窗口均有效
- **And** 当前组件无法读取 API Server CA 或无法计算部署指纹
- **When** 系统计算当前平台权益
- **Then** License 状态必须为 `Unknown`
- **And** reason 必须为 `LicenseEnvironmentInvalid`
- **And** 系统必须回退免费版权益

### Requirement: License 必须包含时间有效期
License 必须 (MUST) 使用 `notBefore` 和 `expiresAt` 表达时间窗口，系统必须按当前时间判断 License 是否有效。

#### Scenario: License 尚未生效
- **Given** 当前时间早于 License claims 中的 `notBefore`
- **When** Operator 校验 License
- **Then** License 状态必须为 `NotYetValid`
- **And** 系统必须回退免费版权益

#### Scenario: License 已过期
- **Given** 当前时间晚于 License claims 中的 `expiresAt`
- **When** Operator 校验 License
- **Then** License 状态必须为 `Expired`
- **And** 系统必须回退免费版权益

#### Scenario: License 在有效期内
- **Given** 当前时间不早于 `notBefore`
- **And** 当前时间不晚于 `expiresAt`
- **When** Operator 校验 License
- **Then** 时间窗口校验必须通过
- **And** 系统可以授予 License claims 中声明的权益

### Requirement: 有效企业 License 必须解除 Cluster 数量限制
当 License 有效且声明 `limits.maxClusters=-1` 时，系统必须 (MUST) 将 `Cluster` 数量上限视为无限制。

#### Scenario: 企业 License 授予无限 Cluster
- **Given** License 已通过签名、产品、指纹和时间校验
- **And** License claims 中 `limits.maxClusters` 为 `-1`
- **When** 系统计算当前平台权益
- **Then** License 状态必须为 `Active`
- **And** `maxClusters` 必须表示无限制

### Requirement: 系统必须输出可查询的 License 状态
Operator 必须 (MUST) 输出脱敏 License 状态，供 server、web 和运维人员查询。

#### Scenario: 状态 ConfigMap 被更新
- **Given** License Secret 被创建、更新或删除
- **When** Operator 完成一次 License 状态计算
- **Then** 必须写入或更新 `disaster-platform-license-status` ConfigMap
- **And** ConfigMap 必须包含 `state`、`edition`、`licenseId`、`expiresAt`、`maxClusters`、`clusterCount`、`reason`、`message` 和 `lastCheckedAt`
- **And** ConfigMap 不得包含私钥、原始指纹信号、kubeconfig 或 token

### Requirement: License 状态 ConfigMap 不得作为门禁可信来源
License 状态 ConfigMap 必须 (MUST) 仅作为展示、诊断和缓存输出；所有真实授权门禁必须基于签名 License Secret 与当前部署指纹计算出的 `Entitlement`。

#### Scenario: 篡改状态 ConfigMap 不影响门禁
- **Given** 当前不存在有效 License
- **And** 用户将 `disaster-platform-license-status` ConfigMap 篡改为 `state=Active` 且 `maxClusters=-1`
- **When** Server、Cluster validating webhook 或 `ClusterReconciler` 判断是否允许新增第 3 个 `Cluster`
- **Then** 系统不得信任该 ConfigMap
- **And** 系统必须基于 License Secret、产品内置公钥和当前部署指纹重新计算 `Entitlement`
- **And** 新增第 3 个 `Cluster` 必须继续按免费版超限处理

#### Scenario: 展示路径可以读取状态 ConfigMap
- **Given** Operator 已写入 `disaster-platform-license-status` ConfigMap
- **When** Web 或运维人员查询 License 展示状态
- **Then** 系统可以返回 ConfigMap 中的脱敏状态
- **And** 该展示状态不得被用于授权放行判定

### Requirement: 跨仓库门禁必须遵循同一 verifier 契约
`disaster-server`、Cluster validating webhook 与 `ClusterReconciler` 必须 (MUST) 使用同一 License verifier 语义，避免 API 放行、webhook 拒绝和 Reconciler 再拒绝的口径漂移。

#### Scenario: Server 添加集群门禁使用可信 verifier
- **Given** 用户通过 `disaster-server` 添加 Cluster
- **When** Server 判断是否允许创建
- **Then** Server 必须基于签名 License Secret 和当前部署指纹计算 `Entitlement`
- **And** Server 不得信任 `disaster-platform-license-status` ConfigMap 作为放行依据
- **And** Server 必须使用与 operator verifier 相同的 canonical JSON、claims schema、指纹和额度算法

#### Scenario: 共享 verifier 不可用时必须通过一致性测试向量
- **Given** 某个仓库无法直接复用 operator 的 verifier 包
- **When** 该仓库实现独立 verifier
- **Then** 必须通过共享 conformance test vectors
- **And** 测试向量必须覆盖 valid、product mismatch、unknown key、unsupported version、malformed limits、expired、fingerprint mismatch、invalid signature 和 ConfigMap tampering

#### Scenario: Server 使用统一 namespace 与 RBAC
- **Given** Chart 配置了 License namespace
- **When** Server 执行添加集群门禁
- **Then** Server 必须从同一 namespace 读取 License Secret
- **And** Server ServiceAccount 必须具备 `get` License Secret 和 `list` Cluster 权限
- **And** Server 不得通过更新 License status ConfigMap 完成门禁

### Requirement: License 失效不得破坏已有容灾链路
License 失效或过期时，系统必须 (MUST NOT) 主动删除已有业务资源或中断已有容灾保护链路。

#### Scenario: License 过期后保留已有 Cluster
- **Given** 系统曾在有效 License 下接受了 3 个 `Cluster`
- **And** License 已过期
- **When** Operator 重新计算权益
- **Then** Operator 不得删除已有 `Cluster`
- **And** Operator 不得仅因 License 过期将已接受存量 `Cluster` 降级为不可维护
- **And** 系统必须禁止继续新增超过免费额度的 `Cluster`

### Requirement: 发证工具必须根据客户指纹生成 License
内部发证工具必须 (MUST) 支持根据客户提交的部署指纹请求生成签名 License 文件。

#### Scenario: 根据指纹请求签发企业 License
- **Given** 客户提交了包含 `product`、`fingerprintVersion` 和 `fingerprint` 的指纹请求
- **When** 内部人员执行发证工具并指定客户、版本、额度和过期时间
- **Then** 工具必须生成 `.lic` 文件
- **And** `.lic` 文件必须包含客户指纹
- **And** `.lic` 文件必须包含可由产品内置公钥验证的签名

#### Scenario: 私钥不得进入客户环境
- **Given** 系统构建官方镜像、chart 或开源仓库
- **When** 执行发布检查
- **Then** 发布产物不得包含发证私钥
- **And** 客户环境不得具备签发新 License 的能力
