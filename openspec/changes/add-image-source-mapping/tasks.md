## 1. 规范与设计

- [x] 1.1 将映射配置入口从 `DisasterInstance` 下沉到 `DisasterConfig` 并更新 proposal/spec delta
- [x] 1.2 固化 `unmatchedPolicy` 默认值为 `Fail`，并明确 `Keep|Fail` 枚举
- [x] 1.3 固化 `applyTo` 首期范围为 `resourceSync|drill`
- [x] 1.4 固化“实例动态读取”时机：每次进入资源恢复构建阶段都读取最新 `DisasterConfig`

## 2. 测试先行（Operator）

- [ ] 2.1 新增 `DisasterConfig.imageRewrite` 解析与校验单元测试（主路径）
- [ ] 2.2 新增 `DisasterConfig.imageRewrite` 错误路径测试（重复 source、空值、别名不存在）
- [ ] 2.3 新增动态读取测试：配置更新后，无需修改实例即可在下一次恢复生效
- [x] 2.4 新增 RestoreBuilder 测试：多容器 + initContainer + 角色切换
- [x] 2.5 新增 `unmatchedPolicy=Fail` 测试：恢复前失败且记录未命中镜像
- [x] 2.6 新增 DataSync trafficless 隔离回归测试

## 3. Operator 实施

- [x] 3.1 保持 `Cluster.spec.imageSources` 能力，补齐与 Config 映射引用一致性的校验
- [x] 3.2 扩展 `DisasterConfig.spec` 新增 `imageRewrite` 字段并更新 CRD
- [x] 3.3 在 ResourceSync/Drill 构建链路按 `instance.spec.config` 动态读取 `DisasterConfig.imageRewrite`
- [x] 3.4 按当前主备角色计算 source/target registry 并生成 `ResourceModifierRules`
- [x] 3.5 明确并实现 `unmatchedPolicy` 行为（`Fail` 前置失败，`Keep` 保留原镜像）
- [x] 3.6 保持 DataSync trafficless 逻辑独立，不混入 Config 映射
- [ ] 3.7 增加事件与状态可观测字段（映射命中数、替换数、失败原因、配置来源）

## 4. Server 实施

- [x] 4.1 扩展 Cluster API DTO/请求体支持 `imageSources`（保持现有能力）
- [x] 4.2 扩展 DisasterConfig API DTO/请求体支持 `imageRewrite`
- [x] 4.3 调整 DisasterInstance API：不再作为 `imageRewrite` 配置入口
- [x] 4.4 增加服务端校验（枚举、重复 source、别名存在性）
- [x] 4.5 补充 handler 单元测试（主路径 + 错误路径）

## 5. Web 实施

- [ ] 5.1 集群配置页维护镜像源目录（`imageSources`）
- [ ] 5.2 容灾基础配置页维护镜像映射（`DisasterConfig.imageRewrite`）
- [ ] 5.3 实例页面展示“动态读取配置”语义，不再录入实例级映射
- [ ] 5.4 增加前端提交前校验与错误提示

## 6. 联调与验收

- [ ] 6.1 端到端验证：resourceSync + drill 的镜像替换生效
- [ ] 6.2 端到端验证：配置更新后实例动态生效
- [ ] 6.3 端到端验证：`unmatchedPolicy=Fail` 的失败可观测性
- [ ] 6.4 回归验证：无映射配置实例行为不变
- [ ] 6.5 更新文档与示例（含 API 示例）
