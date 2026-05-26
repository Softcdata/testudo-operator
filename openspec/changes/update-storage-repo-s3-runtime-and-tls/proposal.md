# Change: 收敛 StorageRepository 的 S3 兼容与 TLS 运行时模型

## Why
当前 `StorageRepository` 链路存在两个割裂点：
1. server 校验接口与 operator/Velero 运行时链路没有统一的 TLS 注入契约。
2. operator 当前默认把 S3 兼容实现收敛为 `path-style=true`，但这一前提没有被正式建模，导致“校验通过但运行失败”风险持续存在。

条目 5 的核心不是简单新增证书字段，而是把以下行为收敛成一套正式契约：
- 哪些 S3 兼容实现属于支持范围
- `path-style` 是默认策略还是显式配置
- CA 如何同时进入验证链路与 Velero runtime

## What Changes

### 1. StorageRepository 引入显式 S3 访问模式
- `StorageRepository` 新增显式访问模式字段，用于区分：
  - `PathStyle`
  - `VirtualHostedStyle`
- 首期默认值保持与现状兼容：`PathStyle`。

### 2. StorageRepository 引入非敏感 CA Secret 引用
- `StorageRepository` 不直接保存证书明文。
- `StorageRepository` 通过非敏感 Secret 引用关联自定义 CA 内容。

### 3. operator 统一校验与运行时的 S3 契约
- `StorageRepositoryReconciler` 的连接校验与 BSL 构造必须使用同一套：
  - endpoint
  - region
  - addressing style
  - TLS/CA
- 不允许出现“校验链路一套、运行时另一套”的行为分叉。

### 4. BSL/Velero runtime 必须实际消费 CA 和 addressing style
- 当配置了自定义 CA 时，operator 必须把 CA 注入到 Velero plugin runtime。
- 当配置了 `VirtualHostedStyle` 时，operator 不得继续强制写死 `s3ForcePathStyle=true`。

### 5. 支持范围以兼容矩阵形式明确
- proposal 明确支持范围至少包含：
  - AWS S3
  - MinIO
  - Ceph RGW
- 对不在兼容矩阵内的实现，不作兼容承诺。

## Non-Goals
- 不在首期支持对象存储厂商的专有高级参数。
- 不修改 quota / 用量统计语义。
- 不改变 patch 接口当前“禁止修改 endpoint”的既有契约，除非 companion proposal 明确同步调整。

## Impact
- Affected specs:
  - `storage-repo`
- Affected code:
  - `pkg/apis/disaster/v1/storagerepository_types.go`
  - `internal/controller/storagerepository_controller.go`
  - `internal/controller/BSL.go`
- Cross-repo impact:
  - `disaster-server`：storage API、验证接口、Secret 契约
  - `cluster-disaster-web`：CA 上传与 addressing style 交互
  - `disaster-system-chart`：若 runtime 注入需要卷或 env，需补模板

## Risks
- 若首期同时引入过多厂商特定例外，会让契约失去边界。
- 若 CA 注入只覆盖 controller 校验而未覆盖 Velero runtime，风险不会真正消除。
