# Tasks

## 1. Proposal
- [x] 1.1 评审 typed refresh 的枚举、signal key 与 signal 生命周期语义
- [x] 1.2 评审 workload namespace 统计字段名与 label key

## 2. Operator
- [x] 2.1 为 `Cluster` status / label contract 增加 workload namespace 统计字段与键名
- [x] 2.2 调整 Cluster controller 的 update predicate，使 `testudo.softcdata.com/refresh-cluster-stats` annotation-only 更新在 Generation 不变时立即入队
- [x] 2.3 实现 `namespaceStats` / `workloadNamespaceStats` / `all` 三种刷新分支
- [x] 2.4 实现基于 `Deployment/StatefulSet` 的 workload namespace 统计
- [x] 2.5 实现 signal 生命周期：成功清理；非法 `type` 终态清理；瞬时错误保留 signal
- [x] 2.6 将 refresh signal annotation 从 metadata hash 与“编辑集群”事件判定中排除
- [x] 2.7 补 update predicate / collector / lifecycle / edit-event exclusion tests

## 3. Alignment
- [x] 3.1 与 server 对齐 action request 的 `type` 枚举、signal key、响应语义、读取字段口径
- [ ] 3.2 与 web 对齐 loading / success / fail 展示

## 4. Verification
- [x] 4.1 `openspec validate add-cluster-namespace-refresh-signal --strict`
