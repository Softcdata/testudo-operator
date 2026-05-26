## ADDED Requirements

### Requirement: 多架构容器镜像构建

系统构建流程 **必须 (SHALL)** 支持构建多种 CPU 架构的容器镜像，包括但不限于 `linux/amd64` 和 `linux/arm64`。

#### Scenario: 构建 ARM64 镜像

- **GIVEN** 开发者在 x86_64 宿主机上
- **WHEN** 执行 `make docker-buildx PLATFORMS=linux/arm64`
- **THEN** 成功构建 ARM64 架构的容器镜像
- **AND** 镜像能在 ARM64 节点上正常运行

#### Scenario: 构建多架构镜像

- **GIVEN** 开发者配置了容器镜像仓库
- **WHEN** 执行 `make docker-buildx PLATFORMS=linux/amd64,linux/arm64`
- **THEN** 构建并推送包含两种架构的 manifest list
- **AND** 不同架构的节点 pull 镜像时自动获取对应架构

#### Scenario: 默认构建保持兼容

- **GIVEN** 现有的 x86 构建流程
- **WHEN** 执行 `make docker-build`
- **THEN** 行为与修改前完全一致
- **AND** 构建的镜像为宿主机架构
