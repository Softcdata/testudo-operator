# Tasks

## 1. Proposal
- [x] 1.1 评审 `addressingStyle` 枚举与默认值
- [x] 1.2 评审 `caSecretRef` 与 operator runtime 注入路径
- [x] 1.3 确认兼容矩阵首期只覆盖 AWS S3 / MinIO / Ceph RGW

## 2. Operator
- [x] 2.1 为 `StorageRepository` 增加 addressing style 与 CA SecretRef 字段
- [x] 2.2 统一校验链路与 BSL 构造链路的 addressing style / TLS 读取逻辑
- [x] 2.3 实现 Velero runtime 的 CA 注入
- [x] 2.4 为 `PathStyle` / `VirtualHostedStyle`、CA 有无两类组合补 controller tests

## 3. Server / Web / Chart Alignment
- [x] 3.1 与 server 对齐 storage API 字段和 Secret 生命周期
- [ ] 3.2 与 web 对齐 addressing style 与 CA 上传交互
- [x] 3.3 若 runtime 注入需要 volume/env，补 chart 方案

## 4. Verification
- [x] 4.1 `openspec validate update-storage-repo-s3-runtime-and-tls --strict`
- [ ] 4.2 用 2-3 个兼容实现完成 create/validate/BSL/backup 端到端验证
