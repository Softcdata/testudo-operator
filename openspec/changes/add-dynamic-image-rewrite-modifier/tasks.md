## 1. Operator API 与 CRD
- [x] 1.1 为 `DisasterInstance.spec.restorePolicy.bulkModifierActions` 增加 `rewriteImage` 动作类型与 `imageRewrite` 字段
- [x] 1.2 更新 deepcopy 与 CRD schema
- [x] 1.3 校验 `rewriteImage` 不要求 `sourceValue/targetValue`，但必须包含有效 `sourcePrefix/targetPrefix`
- [x] 1.4 校验 `applyTo` 支持 `resourceSync`、`drill` 中的实际枚举范围
- [x] 1.5 更新示例 YAML
- [ ] 1.6 评估是否新增独立 `failover` applyTo 枚举

## 2. 运行时镜像扫描与编译
- [x] 2.1 实现源集群资源 spec 镜像字段扫描器
- [x] 2.2 扫描器跳过 `/status/**`、`/metadata/finalizers/**`、`/metadata/ownerReferences/**`
- [x] 2.3 实现 `rewriteImage` 编译器：读取当前镜像、匹配前缀、生成目标镜像
- [x] 2.4 实现最长前缀优先和目标冲突失败关闭
- [x] 2.5 实现 `unmatchedPolicy=Keep|Fail`
- [x] 2.6 生成符合现有 `reversible pair` contract 的运行时规则

## 3. Restore Builder 集成
- [x] 3.1 ResourceSync 恢复构建阶段动态编译 `rewriteImage`
- [x] 3.2 Drill 资源恢复构建阶段动态编译 `rewriteImage`
- [ ] 3.3 Failover/DisasterOperation 恢复构建阶段动态编译 `rewriteImage`
- [x] 3.4 合并运行时生成规则、手写规则和既有快照规则，保持优先级与冲突语义确定
- [x] 3.5 确认 `rewriteImage` 不因缺少长期 `modifierRuleSnapshot` 触发 bulk snapshot 失败关闭

## 4. 审计与 Preview
- [ ] 4.1 在 AppRestore 或操作状态中记录运行时编译摘要
- [ ] 4.2 发射结构化事件：匹配数量、生成规则数量、跳过 forbidden path 数量、未匹配数量
- [ ] 4.3 在 disaster-server 增加或扩展 modifier preview API
- [ ] 4.4 Web 展示当前源镜像、目标镜像、跳过路径和冲突错误

## 5. 测试
- [x] 5.1 单测：tag 变化后同一 DSL 动态生成新规则
- [x] 5.2 单测：digest 镜像保留 digest suffix
- [x] 5.3 单测：Pod status image 不进入生成规则
- [x] 5.4 单测：最长前缀优先
- [x] 5.5 单测：同一资源路径目标冲突失败
- [x] 5.6 E2E：源镜像从 `v1.30.0` 改为 `v1.31.0` 后，不修改 DSL 再次同步仍成功重写
- [x] 5.7 E2E：`unmatchedPolicy=Fail` 返回未命中镜像明细
- [x] 5.8 单测：`unmatchedPolicy=Fail` 返回未命中镜像明细
- [x] 5.9 回归：实例包含 `rewriteImage` 且 Drill 未提供 `restorePolicy` 覆盖时，创建的 `AppRestore` 包含多个 initContainer 运行时规则
- [ ] 5.10 Web E2E：两个 Drill 级修改开关关闭时创建请求不携带 `restorePolicy`；开启任一开关时携带 Drill 覆盖

## 6. 文档与废弃路径
- [x] 6.1 更新用户文档，说明动态镜像重写替代完整值镜像替换
- [x] 6.2 标注旧 `imageSources/imageRewrite` 镜像源映射不再作为推荐路径
- [x] 6.3 补充从 `replaceExactValue` 完整镜像规则迁移到 `rewriteImage` 的示例
- [ ] 6.4 更新 OpenAPI / RunAPI / Apipost 文档
