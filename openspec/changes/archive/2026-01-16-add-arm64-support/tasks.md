# 任务清单: 添加 ARM64 架构支持

## 1. disaster-operator 修改

### 1.1 Dockerfile 优化
- [x] 1.1.1 修改构建阶段，添加 `--platform=${BUILDPLATFORM}` 确保在宿主机架构编译
- [x] 1.1.2 修改运行时阶段，确保使用目标架构的基础镜像
- [x] 1.1.3 解决 Helm 二进制架构问题：在构建时根据 `TARGETARCH` 动态下载对应版本
- [x] 1.1.4 本地测试 `docker build --platform linux/arm64` 构建成功

### 1.2 Makefile 优化
- [x] 1.2.1 简化 `PLATFORMS` 默认值为 `linux/amd64,linux/arm64`（移除 s390x, ppc64le）
- [x] 1.2.2 添加 `docker-build-arm64` 便捷目标用于单独构建 ARM64 镜像
- [x] 1.2.3 移除 sed 生成 Dockerfile.cross 的逻辑（Dockerfile 已原生支持）

## 2. disaster-server 修改

### 2.1 Dockerfile 优化
- [x] 2.1.1 添加 `--platform=${BUILDPLATFORM}` 到构建阶段
- [x] 2.1.2 确保 `TARGETOS` 和 `TARGETARCH` 正确传递并使用
- [x] 2.1.3 验证 `distroless/static:nonroot` 支持多架构（已支持）
- [x] 2.1.4 本地测试 `docker build --platform linux/arm64` 构建成功

### 2.2 构建脚本创建
- [x] 2.2.1 创建 `scripts/docker-build.sh` 封装 Docker 构建命令
- [x] 2.2.2 支持参数: `--platform`, `--push`, `--tag`
- [x] 2.2.3 添加 `--buildx` 选项用于多架构构建
- [ ] 2.2.4 更新 `README.md` 说明多架构构建方式

## 3. 验证测试

- [x] 3.1 在 x86 机器上测试 `TARGETARCH=arm64` 交叉编译
- [ ] 3.2 在 ARM64 节点上测试镜像启动
- [x] 3.3 验证现有 x86 构建流程不受影响

## 4. 文档更新

- [ ] 4.1 更新 disaster-operator README 说明多架构构建
- [ ] 4.2 更新 disaster-server README 说明多架构构建
- [ ] 4.3 添加架构支持说明到部署文档
