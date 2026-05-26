# 技术设计: ARM64 架构支持

## Context

当前灾备系统仅支持 x86_64 (amd64) 架构。需要扩展支持 ARM64 架构以适应：
- 云厂商 ARM 实例（AWS Graviton, Azure Arm-based VMs）
- 国产化 ARM 服务器（华为鲲鹏、飞腾等）
- 边缘计算场景

## Goals / Non-Goals

### Goals
- 支持构建 `linux/arm64` 架构的容器镜像
- 保持与现有 `linux/amd64` 构建的完全兼容
- 提供简单的命令行接口进行多架构构建

### Non-Goals
- 不在本提案中处理 CI/CD 流水线改造（后续工作）
- 不支持 s390x, ppc64le 等小众架构（可后续按需添加）
- 不改变应用运行逻辑

## Decisions

### Decision 1: 使用 Docker BuildX 进行多架构构建

**选择**: 使用 Docker BuildX + QEMU 模拟

**原因**:
- BuildX 是 Docker 官方推荐的多架构构建方案
- QEMU 用户态模拟可在 x86 机器上构建 ARM 镜像
- 无需专门的 ARM 构建机器

**替代方案**:
- 原生 ARM 构建机: 速度更快但需要额外基础设施
- Manifest List 拼接: 需要分别在不同机器构建再合并，复杂度高

### Decision 2: Helm 二进制动态下载

**选择**: 在 Dockerfile 构建阶段根据 `TARGETARCH` 下载对应架构的 Helm

**实现**:
```dockerfile
ARG TARGETARCH
RUN HELM_ARCH=$(echo ${TARGETARCH} | sed 's/amd64/amd64/' | sed 's/arm64/arm64/') && \
    curl -fsSL https://get.helm.sh/helm-v3.14.0-linux-${HELM_ARCH}.tar.gz | tar xz && \
    mv linux-${HELM_ARCH}/helm /usr/local/bin/helm
```

**原因**:
- 当前 `COPY helm` 的方式只能使用宿主机架构的 Helm
- 动态下载确保 Helm 与目标架构匹配

**替代方案**:
- 使用 Helm 官方多架构镜像作为中间层: 增加构建复杂度
- 不打包 Helm，运行时下载: 增加启动时间和网络依赖

### Decision 3: 基础镜像选择

| 项目 | 构建镜像 | 运行时镜像 | 多架构支持 |
|------|---------|-----------|-----------|
| disaster-operator | `golang:1.24` | `alpine:latest` | ✅ 官方支持 |
| disaster-server | `golang:1.24.5-alpine` | `distroless/static:nonroot` | ✅ 官方支持 |

所有基础镜像均已支持多架构，无需更换。

### Decision 4: 默认平台范围

**选择**: 默认只构建 `linux/amd64,linux/arm64`

**原因**:
- s390x, ppc64le 用户极少
- 减少构建时间（每增加一个平台约增加 1.5x 时间）
- 可通过 `PLATFORMS` 变量按需扩展

## Dockerfile 修改方案

### disaster-operator/Dockerfile

```dockerfile
# Build the manager binary
FROM --platform=${BUILDPLATFORM} docker.1ms.run/library/golang:1.24 AS builder
ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /workspace

# Copy Go modules manifests
COPY go.mod go.sum ./
RUN GOPROXY=https://goproxy.cn,direct go mod download

# Copy source
COPY cmd/main.go cmd/main.go
COPY pkg/ pkg/
COPY internal/ internal/

# Download Helm for target architecture
ARG HELM_VERSION=v3.14.0
RUN HELM_ARCH=${TARGETARCH} && \
    curl -fsSL https://get.helm.sh/helm-${HELM_VERSION}-linux-${HELM_ARCH}.tar.gz | tar xz && \
    mv linux-${HELM_ARCH}/helm /usr/local/bin/helm

COPY velero-11.1.1.tgz .

# Build for target platform
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go

# Runtime image
FROM --platform=${TARGETPLATFORM} docker.1ms.run/library/alpine:latest

USER 1000:1000
WORKDIR /app
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/velero-11.1.1.tgz .
COPY ./velero.values.yaml .
COPY --from=builder /usr/local/bin/helm /usr/local/bin/helm

ENTRYPOINT ["/app/manager"]
```

### disaster-server/Dockerfile

```dockerfile
FROM --platform=${BUILDPLATFORM} golang:1.24.5-alpine AS builder
WORKDIR /src

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev

# Install dependencies
RUN apk add --no-cache git openssl ca-certificates

# ... (existing cert and git config) ...

# Download dependencies
COPY go.mod go.sum ./
RUN go env -w GOPROXY=https://goproxy.cn,direct && \
    go env -w GOPRIVATE='github.com/softcdata/testudo-operator' && \
    go mod download

COPY . .

# Build for target platform
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -mod=mod -trimpath \
    -ldflags "-s -w -X github.com/softcdata/testudo-server/cmd/app.Version=${VERSION}" \
    -o /out/disaster ./

# Runtime image (multi-arch supported)
FROM --platform=${TARGETPLATFORM} gcr.io/distroless/static:nonroot

WORKDIR /app
COPY --from=builder /out/disaster /usr/local/bin/disaster
COPY configs/config.yaml /app/configs/config.yaml

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/disaster", "server"]
```

## Risks / Trade-offs

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| QEMU 模拟构建慢 | 构建时间增加 2-3x | 仅在需要时构建 ARM64 |
| Helm 下载失败 | 构建失败 | 使用代理或预下载 |
| 私有仓库交叉编译 | 可能失败 | CGO_ENABLED=0 纯 Go 编译无问题 |

## Migration Plan

1. **Phase 1**: 修改 Dockerfile，本地验证
2. **Phase 2**: 更新 Makefile 和构建脚本
3. **Phase 3**: 文档更新
4. **Phase 4**: (后续) CI/CD 流水线改造

## Open Questions

- [ ] 是否需要在 CI 中自动构建多架构镜像？
- [ ] ARM64 镜像的测试覆盖如何保证？
