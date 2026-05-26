# Change: 添加平台授权与集群数量门禁

## Why
当前项目正在做开源准备，源码协议已倾向采用 Apache-2.0。Apache-2.0 适合开放源码和企业采用，但它不适合承载“免费版最多添加 2 个集群”的商业限制；该限制应作为官方发行版、企业授权与商业支持条款的一部分，由产品授权机制表达。

### 开源协议严格程度与适用性

| 协议 | 严格程度 | 核心特性 | 对本项目适用程度 | 备注 |
| --- | --- | --- | --- | --- |
| `MIT` | 很宽松 | 保留版权与免责声明；允许商用、修改、闭源集成和再分发 | 中 | 简单易用，但专利授权不如 Apache-2.0 明确；不适合作为企业基础设施项目的首选 |
| `BSD-2-Clause` / `BSD-3-Clause` | 很宽松 | 与 MIT 类似；BSD-3 额外限制不得用原作者/组织名义背书 | 中 | 企业友好，但同样缺少 Apache-2.0 的明确专利授权结构 |
| `Apache-2.0` | 宽松 | 允许商用、修改、闭源集成和再分发；包含明确专利授权、NOTICE 与专利诉讼终止机制 | 高，首选 | 最贴近 Kubernetes / Kubebuilder / Velero 生态；适合开源仓库和企业私有化采用 |
| `MPL-2.0` | 中等，文件级弱 copyleft | 修改 MPL 文件并分发时，需要开放这些文件的修改；仍允许与闭源文件组合 | 中高，备选 | 比 Apache-2.0 更强调核心文件改动回馈，但企业法务接受成本略高 |
| `LGPL-3.0` | 中等，库级弱 copyleft | 主要约束库的修改和链接方式；允许一定程度闭源集成 | 低 | 更适合库项目，不太适合 operator / 平台主程序 |
| `GPL-3.0` | 强 copyleft | 分发修改版或衍生作品时，通常需要按 GPL 提供对应源码 | 中低 | 能防闭源二进制/镜像发行版，但会显著提高企业采用门槛；不覆盖纯网络服务不分发场景 |
| `AGPL-3.0` | 最严格的主流 OSI 开源 | 在 GPL-3.0 基础上增加网络交互场景的源码提供义务 | 低到中，战略型选择 | 适合防止云厂商/竞品闭源托管改造，但对私有化客户、生态集成和商业合作阻力最大 |
| `BSL-1.1` / `FSL` / `Elastic License 2.0` | 商业限制强，但不是标准 OSI 开源 | 可限制商业使用、托管服务、规模或时间窗口；通常带后续转换或专有条款 | 仅作为 source-available 备选 | 如果必须强制“免费版最多 2 个集群”，应走这类 source-available / 商业源代码路线，而不是标准开源路线 |

本提案推荐继续采用 `Apache-2.0` 作为源码协议，原因是它最符合当前 Kubernetes operator 的生态预期和企业采用诉求。若后续希望更强调核心代码改动回馈，可以评估 `MPL-2.0`；若战略目标转向防止云服务商闭源托管改造，则需要重新评估 `AGPL-3.0` 对整个产品矩阵（operator、server、web、chart、CLI）的影响。无论选择 Apache-2.0、MPL-2.0、GPL-3.0 或 AGPL-3.0，标准开源协议都不应承载“最多 2 个集群”这类使用规模限制。

`disaster-operator` 的核心收费维度初步确定为受管 `Cluster` 数量：
- 默认无授权时，平台只允许添加 2 个 `Cluster`。
- 安装有效企业 License 后，解除 `Cluster` 数量限制。
- License 必须绑定客户部署环境指纹，并具备时间有效期。

如果只在前端或 server 做限制，用户仍可通过 `kubectl apply` 直接创建 `Cluster`。如果只在 operator 做限制，用户体验和错误反馈会滞后。因此需要定义一个跨 Operator / Server / Web / Chart / 工具链的授权能力边界。

## What Changes

### 0. 交付边界
- 本 change 定义为完整跨仓库交付，不是仅 Operator MVP。
- 归档前必须完成 Operator、Server、Web、Chart、Cluster validating webhook、客户侧工具、内部发证工具和跨仓库验证。
- 实现可以拆成多个短 PR，但这些 PR 都归属于 `add-platform-license-gate`；不得在只完成 Operator 兜底后归档本 change。
- 在线激活、在线续费、在线撤销和非 Cluster 数量类 feature entitlement 不在本 change 内。

### 1. 新增平台授权模型
- 定义 `Platform License` 作为产品授权能力。
- License 文件使用 `.lic` 后缀，内容为 JSON。
- License 通过 Ed25519 离线签名，产品内置公钥，发证侧持有私钥。
- License 签名 payload 必须使用 RFC 8785 JCS canonical JSON；`signature` 使用无 padding base64url 编码。
- License claims 至少包含：
  - `licenseId`
  - `product`
  - `customer`
  - `edition`
  - `fingerprintVersion`
  - `fingerprint`
  - `issuedAt`
  - `notBefore`
  - `expiresAt`
  - `limits.maxClusters`
  - `features`
  - `keyId`
  - `signature`
- License claims 必须校验 `product`、`version`、`keyId`、`fingerprintVersion`、`limits.maxClusters` 与 UTC RFC3339 时间格式；任一关键字段缺失、不支持或不匹配时，必须回退免费版权益。

### 2. 默认免费版权益
- 未安装 License、License 无效、签名错误、指纹不匹配、尚未生效或已过期时，系统按免费版权益运行。
- 免费版默认 `maxClusters=2`。
- 企业版有效 License 可设置 `limits.maxClusters=-1`，表示集群数量无限制。

### 3. Kubernetes 部署指纹
- 首期指纹采用 `k8s-v1`，绑定管理集群部署环境，而不是绑定某个 Pod 所在 Node。
- 指纹由以下信号计算：
  - `kube-system` namespace UID
  - 平台安装 namespace UID
  - API Server CA SHA256
  - 平台 `install-id` Secret
- 客户侧工具只输出指纹 hash，不泄露 kubeconfig、token、CA 明文或原始 UID。

### 4. License 存储与状态输出
- License 安装到管理集群 Secret：
  - `namespace`: operator manager namespace，默认 `disaster-system`
  - `name`: `disaster-platform-license`
  - `type`: `testudo.softcdata.com/license`
  - `data/license.lic`
- Operator 校验 License 后，输出状态 ConfigMap：
  - `name`: `disaster-platform-license-status`
  - `state`: `Free|Active|Expired|InvalidSignature|FingerprintMismatch|NotYetValid|Malformed|UnsupportedVersion|ProductMismatch|UnknownKey|Unknown`
  - `edition`
  - `licenseId`
  - `customer`
  - `expiresAt`
  - `maxClusters`
  - `clusterCount`
  - `fingerprintMatched`
  - `reason`
  - `message`
  - `lastCheckedAt`
- 状态 ConfigMap 仅作为展示、诊断与缓存输出，不得作为授权门禁的可信来源。
- 所有真实门禁路径必须基于签名 License Secret、产品内置公钥与当前部署指纹重新计算 `Entitlement`，或调用等价的可信 verifier 路径。

### 5. 添加集群门禁
- Server 在添加集群 API 前置校验 License 与当前 `Cluster` 数量，直接返回用户可理解的错误。
- Cluster validating webhook 在 Kubernetes admission 阶段拦截超限 `Cluster` 创建。
- `ClusterReconciler` 作为最终兜底：即使 webhook 未启用，也不得让超限新增 `Cluster` 推进到 `Ready` 或触发 Velero 安装。
- Server、Cluster validating webhook 与 `ClusterReconciler` 不得信任 `disaster-platform-license-status` ConfigMap 中的 `state` 或 `maxClusters` 作为放行依据。
- Server 与 webhook 使用创建前未删除 `Cluster` 数量判断是否允许创建。
- `ClusterReconciler` 发生在 CR 已落库之后，必须按已接受的兄弟对象数量判断当前未接受对象是否还能被接受，并在任何外部副作用前写入接受态标记。
- 接受态标记是强契约，不是可选优化；并发接受流程必须串行化，避免多个直写 `Cluster` 同时占用同一额度。

### 6. 过期与失效安全策略
- License 过期或失效不得删除已有 `Cluster`。
- License 过期或失效不得主动中断已有备份、恢复、故障切换等容灾链路。
- License 过期或失效后，系统禁止新增超过免费额度的 `Cluster`。
- 已被授权接受的存量 `Cluster` 应允许继续维护、删除和基础状态收敛。
- 首次启用 license gate 时，升级前已存在的 `Cluster` 必须被 grandfather 并回填接受态标记，避免已有现场因缺少新注解而被误判为新增超限对象。

### 7. 发证与客户侧工具
- 客户侧工具生成部署指纹请求：
  - `disasterctl license fingerprint`
- 内部发证工具根据指纹生成 `.lic`：
  - `disaster-license issue`
- 客户侧可安装和查看授权状态：
  - `disasterctl license install`
  - `disasterctl license status`

## Non-Goals
- 不改变源码协议；源码仍按 Apache-2.0 开源。
- 不把“2 个集群限制”写成 Apache-2.0 的附加限制。
- 不要求首期实现在线激活、在线续费或在线撤销。
- 不在首期实现复杂按功能模块计费；首期只约束 `Cluster` 数量。
- 不在 License 失效时删除存量业务资源或停止已有容灾保护链路。
- 不把私钥放入开源仓库、镜像、Helm chart 或客户环境。
- 不把仅 Operator 兜底视为本 change 完成；server/web/webhook/chart/tooling 必须完成后才能归档。

## Impact
- Affected specs:
  - `platform-license`（新增）
  - `cluster`
- Affected repos / components:
  - `disaster-operator`
    - License 校验、公钥、指纹、状态输出、ClusterReconciler 兜底、Cluster validating webhook
  - `disaster-server`
    - License 状态 API、License 上传、添加集群前置门禁、共享 verifier 契约
  - `cluster-disaster-web`
    - 授权状态展示、License 上传、超限提示
  - `disaster-system-chart`
    - License Secret 模板、状态 ConfigMap/RBAC、webhook 默认安装策略
  - internal tooling
    - 发证工具、keygen、verify、fingerprint

## Relationship to Apache-2.0
- Apache-2.0 适用于开源仓库中的源码、manifest 与文档，除非文件另有说明。
- 商业 License 约束官方企业版、官方发行物、企业订阅、支持服务与授权文件。
- 商业 License 不应作为 Apache-2.0 源码许可证的附加限制。
- 如果用户基于 Apache-2.0 源码 fork 并移除限制，源码许可证层面不阻止；商业保护依赖官方镜像、商标、支持、企业功能与商业合同。

## Risks
- 纯开源代码中的门禁不能防止用户 fork 后删除限制，需要在商业叙述中明确官方发行版与开源源码的边界。
- 如果只实现 Reconciler 兜底，API 和 UI 的用户体验不足，需要后续补 server/web/webhook。
- 指纹绑定过强会导致客户重装、迁移后频繁失效；首期必须采用 Kubernetes 部署指纹和 `install-id` 组合，避免绑定单个 Node。
- License 状态若不可用时按免费版处理，可能导致临时误拦截第 3 个集群创建；需要清晰错误与可观测状态。
- License 状态 ConfigMap 位于客户集群内，可能被具备足够 RBAC 的主体篡改；真实授权门禁必须避免信任该 ConfigMap。
- 如果不定义 accepted sibling count 与升级 grandfather 规则，Reconciler 可能误拒免费版第 2 个直写 `Cluster`，或误伤升级前已有存量 `Cluster`。
- 如果 server 与 operator 不共享 verifier 语义，可能出现 API 放行、webhook 拒绝、Reconciler 再拒绝的用户体验漂移。
- 如果 canonical JSON、签名编码或 CA hash 输入不精确定义，发证工具、客户侧指纹工具与 operator verifier 可能计算出不同结果。
