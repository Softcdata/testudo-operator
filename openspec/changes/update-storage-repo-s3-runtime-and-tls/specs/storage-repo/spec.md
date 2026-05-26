## MODIFIED Requirements

### Requirement: 存储库 BSL 生成
StorageRepository 必须 (MUST) 能在目标集群生成对应的 `BackupStorageLocation`，并对齐已声明的 S3 addressing style 与 TLS 信任源。

#### Scenario: 以 PathStyle 方式生成 BSL
- **Given** 一个 `StorageRepository` 显式声明 `addressingStyle=PathStyle`
- **When** operator 同步 BSL 到远端集群
- **Then** 生成的 BSL 必须使用 path-style 访问语义

#### Scenario: 以 VirtualHostedStyle 方式生成 BSL
- **Given** 一个 `StorageRepository` 显式声明 `addressingStyle=VirtualHostedStyle`
- **When** operator 同步 BSL 到远端集群
- **Then** 生成的 BSL 不得继续强制写死 `s3ForcePathStyle=true`
- **And** 必须按 virtual-hosted-style 语义工作

#### Scenario: 配置自定义 CA 时运行时必须消费该 CA
- **Given** 一个 `StorageRepository` 关联了 CA Secret 引用
- **When** operator 同步 BSL 到远端集群
- **Then** Velero plugin runtime 必须能够读取并使用该 CA
- **And** 运行时 TLS 行为必须与 controller 校验链路保持一致
