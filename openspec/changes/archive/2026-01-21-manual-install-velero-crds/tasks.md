# Tasks: 手动安装 Velero CRDs (Manual Installation of Velero CRDs)

## Development (开发)
- [x] **资源准备 (Asset Preparation)**:
    - [x] 确保 `dist/velero-crds.yaml` 文件存在。
    - [x] 修改 `Dockerfile`：添加 `COPY ./dist/velero-crds.yaml .`。
- [x] **控制器逻辑 (Controller Logic)**:
    - [x] 创建 `velero_helpers.go` 实现 `EnsureVeleroCRDs` 函数。
    - [x] 逻辑应按 `---` 分割 YAML 并应用每个资源 (CRD)。
    - [x] 在 `InstallVeleroInCluster` 的 `CommandExecutor.Run("helm", "upgrade", ...)` 之前调用 `EnsureVeleroCRDs`。
    - [x] **重要**: 将 `--no-hooks` 加回 `helm upgrade` 命令参数中。
    - [x] 为每个安装步骤添加详细的日志记录。

## Testing (测试)
- [x] 编译通过 (`go build ./...`)
- [ ] 单元测试通过
- [ ] E2E 验证: Velero 安装成功且没有出现 `velero-upgrade-crds` Pod

## Cleanup (清理)
- [ ] 移除任何临时的 `velero.values.yaml` 镜像 tag hacks（恢复为标准 values 或保留作为 fallback）。
