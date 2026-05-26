# Change: 添加 ARM64 架构支持

## Why

当前所有环境都运行在 x86 (amd64) 架构上。随着 ARM 服务器（如 AWS Graviton、华为鲲鹏）的普及，需要支持在 ARM64 架构上部署灾备系统。这要求 `disaster-operator` 和 `disaster-server` 的容器镜像能够跨平台构建并运行。

## What Changes

### disaster-operator

- **优化 Dockerfile**: 确保多阶段构建对 ARM64 兼容
  - 使用 `--platform=${BUILDPLATFORM}` 确保构建阶段在宿主机架构上运行
  - 运行时镜像使用 `--platform=${TARGETPLATFORM}` 确保目标架构正确
- **修复 Helm 二进制依赖**: 
  - 当前 Dockerfile 直接 `COPY helm`，但 Helm 是架构相关的二进制文件
  - 需要在构建时下载对应架构的 Helm，或在运行时动态下载
- **velero chart 无需修改**: `velero-11.1.1.tgz` 是纯 Helm chart（YAML 模板），架构无关；Velero 官方镜像 `velero/velero:v1.17.0` 已支持多架构
- **更新 Makefile**: 优化 `docker-buildx` 目标，默认只构建 `linux/amd64,linux/arm64`

### disaster-server

- **创建多架构 Dockerfile**: 添加 `--platform` 指令支持跨平台构建
- **创建构建脚本**: 添加 `scripts/docker-build.sh` 支持单架构和多架构构建
- **验证依赖**: 确保私有仓库依赖在交叉编译时正常工作

## Impact

- **受影响规范**: 无（纯基础设施变更）
- **受影响代码**:
  - `disaster-operator/Dockerfile`
  - `disaster-operator/Makefile`
  - `disaster-server/Dockerfile`
  - `disaster-server/scripts/docker-build.sh` (新增)
- **兼容性**: 不影响现有 x86 构建，ARM64 为增量支持
- **CI/CD**: 可能需要更新流水线以支持多架构推送（不在本提案范围）

## Risks

- **构建时间增加**: 多架构构建比单架构慢约 2-3 倍
- **Helm 二进制**: 需要在构建时动态下载对应架构的 Helm
- **私有仓库**: `disaster-server` 的私有 Git 依赖在容器内交叉编译需测试验证

## Success Criteria

1. `make docker-buildx PLATFORMS=linux/arm64` 能成功构建 ARM64 镜像
2. `make docker-buildx PLATFORMS=linux/amd64` 与现有行为一致
3. 构建的 ARM64 镜像能在 ARM64 节点上正常启动
