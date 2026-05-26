# Tasks

## 1. Proposal
- [x] 1.1 明确 Apache-2.0 开源协议与商业 License 的边界
- [x] 1.2 明确免费版 2 个 Cluster、企业版无限 Cluster 的首期权益模型
- [x] 1.3 明确 License JSON + Ed25519 签名格式
- [x] 1.4 明确 `k8s-v1` Kubernetes 部署指纹
- [x] 1.5 明确 Secret / ConfigMap 存储与状态输出模型
- [x] 1.6 明确 Server / Webhook / Reconciler 三层门禁
- [x] 1.7 明确状态 ConfigMap 仅为展示缓存，不得作为授权门禁可信来源
- [x] 1.8 明确 accepted sibling count、接受态标记和升级 grandfather 算法
- [x] 1.9 明确 License claims 负向校验、canonical JSON、CA hash 和跨仓库 verifier 契约
- [x] 1.10 明确本 change 为完整跨仓库交付，不能在仅完成 Operator MVP 后归档

## 2. Operator
- [x] 2.1 新增 `pkg/license`，实现 claims、canonical JSON、Ed25519 验签与 Entitlement 转换
- [x] 2.2 实现 License claims schema 校验：product、version、keyId、fingerprintVersion、时间格式和 `limits.maxClusters`
- [x] 2.3 实现 RFC 8785/JCS 受约束 profile payload、无 padding base64url signature 和共享 conformance test vectors
- [x] 2.4 实现 `k8s-v1` 指纹采集、确定性 API Server CA hash 与 `install-id` Secret 生命周期
- [x] 2.5 读取 `disaster-platform-license` Secret 并输出 `disaster-platform-license-status` ConfigMap
- [x] 2.6 在 `ClusterReconciler` 中实现 accepted sibling count 兜底限制
- [x] 2.7 为已接受 Cluster 强制写入 `license-accepted` 注解，避免 License 过期误降级存量集群
- [x] 2.8 串行化 Cluster 接受流程，确保 accepted count 与接受态标记写入不会并发超发
- [x] 2.9 实现首次启用 license gate 的 grandfather 回填与 `enabledAt` 状态记录
- [x] 2.10 增加 Cluster validating webhook，拦截超限 CREATE
- [x] 2.11 补充 License 验签、product mismatch、unknown key、unsupported version、malformed limits、过期、指纹不匹配、集群限制与存量保护单元测试
- [x] 2.12 补充篡改 `disaster-platform-license-status` ConfigMap 不影响 Reconciler 门禁的测试

## 3. Server
- [x] 3.1 增加 License 状态查询接口
- [x] 3.2 增加 License 上传/安装接口，写入管理集群 Secret
- [x] 3.3 在添加集群 API 前置校验 License 与当前 Cluster 数量
- [x] 3.4 定义超限、签名错误、指纹不匹配、过期等稳定错误码和响应文案
- [x] 3.5 确保添加集群门禁不信任 License 状态 ConfigMap，只使用可信 verifier 结果
- [x] 3.6 复用共享 verifier 包或通过共享 conformance test vectors
- [x] 3.7 明确 License namespace 配置、RBAC、Cluster 计数口径和 License schema version 兼容策略
- [x] 3.8 补充 handler/service 测试

## 4. Web
- [ ] 4.1 展示当前 License 状态、版本、到期时间、集群额度和当前集群数
- [ ] 4.2 添加 License 上传入口
- [ ] 4.3 添加第 3 个集群时展示社区版额度限制与企业授权提示
- [ ] 4.4 到期前和到期后展示可操作提示

## 5. Chart / Deploy
- [x] 5.1 增加 License Secret 模板或安装说明
- [x] 5.2 增加 License status ConfigMap/RBAC 权限
- [x] 5.3 将 Cluster validating webhook 纳入生产安装路径
- [x] 5.4 记录私钥不得进入 chart、镜像或仓库的发布检查项
- [x] 5.5 限制普通用户只能读取 License status ConfigMap，不得更新状态 ConfigMap 或读取 License Secret
- [x] 5.6 配置统一 License namespace，并同步给 operator、server 和 chart 资源
- [ ] 5.7 通过真实 Helm 安装/升级验证运行时 Cluster validating webhook 已创建并可拦截直接创建第 3 个 Cluster

## 6. Tooling
- [x] 6.1 实现客户侧 `disasterctl license fingerprint`
- [x] 6.2 实现客户侧 `disasterctl license install`
- [x] 6.3 实现客户侧 `disasterctl license status`
- [x] 6.4 实现内部 `disaster-license keygen`
- [x] 6.5 实现内部 `disaster-license issue`
- [x] 6.6 实现内部 `disaster-license verify`
- [x] 6.7 输出发证工具、客户指纹工具、operator verifier、server verifier 共享的 conformance test vectors

## 7. Verification
- [x] 7.1 `openspec validate add-platform-license-gate --strict`
- [x] 7.2 `make harness-ci`
- [x] 7.3 Operator 定向单元测试
- [x] 7.4 Server 定向单元测试
- [ ] 7.5 Web typecheck / lint
- [ ] 7.6 真实环境 runbook：无 License 创建第 1/2/3 个 Cluster
- [ ] 7.7 真实环境 runbook：有效企业 License 创建第 3 个 Cluster
- [ ] 7.8 真实环境 runbook：License 过期后不破坏存量 Cluster，禁止新增超限 Cluster
- [ ] 7.9 真实环境 runbook：已有 3 个未注解 Cluster 升级后被 grandfather，新增第 4 个仍受限制
- [ ] 7.10 真实环境 runbook：Helm 安装/升级后 `disaster-operator-validating-webhook-configuration` 存在，且 direct `kubectl create cluster` 第 3 个 Cluster 被 admission 拒绝
- [ ] 7.11 归档前确认 Operator、Server、Web、Chart、webhook、客户侧工具、内部发证工具和 conformance test vectors 均已完成
