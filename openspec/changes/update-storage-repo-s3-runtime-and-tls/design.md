# Design: Storage Repo S3 Runtime and TLS

## 背景
当前 operator 校验和运行时都带着 `path-style=true` 的隐含前提，但并没有一个正式字段来表达这件事。同时，自定义 CA 也没有稳定注入路径。

本设计的目标是把 S3 兼容问题拆成两个显式维度：
- addressing style
- TLS trust source

## 关键决策

### D1. 首期显式字段只引入 `addressingStyle`
- 枚举值：`PathStyle`、`VirtualHostedStyle`
- 默认值：`PathStyle`
- 原因：
  - 与当前实现兼容
  - 可以把当前硬编码行为变成显式契约

### D2. 自定义 CA 通过 SecretRef 建模
- `StorageRepository` 仅保存 `caSecretRef`
- Secret 存放在 operator namespace，由 server 负责维护
- operator 负责读取并注入 runtime

### D3. 运行时注入优先选择 BSL / Velero plugin 可消费路径
- CA 不是只给 controller 校验用。
- 需要把 CA 放到 Velero plugin 实际可读取的位置，确保备份和恢复运行时与验证链路一致。

## 兼容矩阵原则
- 全局字段默认值：`PathStyle`
- AWS S3：支持 `VirtualHostedStyle` 与 `PathStyle`，首期默认值仍保持 `PathStyle` 以兼容现有行为
- MinIO：默认 `PathStyle`
- Ceph RGW：默认 `PathStyle`
- 其余实现：不做兼容承诺

## 生命周期
- create/update：
  - 读取 addressing style
  - 读取 CA SecretRef
  - 统一应用到校验与 BSL runtime
- rotate CA：
  - controller 检测 Secret 变化
  - 重建或刷新 BSL runtime 注入

## 备选方案

### 方案 A：继续固定 path-style
- 放弃原因：无法支持明确需要 virtual-hosted-style 的兼容实现。

### 方案 B：把 CA 明文写进 CR
- 放弃原因：敏感配置不应进入 CRD。

### 方案 C：只在 server 验证接口支持 CA
- 放弃原因：不能解决 runtime 分叉问题。
