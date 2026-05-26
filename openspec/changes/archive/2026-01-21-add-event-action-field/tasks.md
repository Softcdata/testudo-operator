# 任务清单：建立全局事件规范

## 0. BDD 测试先行

- [x] 0.1 **创建 BDD 集成测试文件**
  - 文件：`internal/controller/global_event_test.go`
  - 测试覆盖：
    - 事件消息格式验证
    - Task 名称格式验证
    - Cluster/Storage/AppBackup/AppRestore 事件格式
  - 状态：✅ 已完成

- [x] 0.2 **运行测试（预期失败）**
  - 当前测试会跳过/失败，因为功能未实现
  - 命令：`go test ./internal/controller -v --ginkgo.v --ginkgo.focus="Global Event Specification"`
  - 状态：✅ 格式测试通过

## 1. Cluster Controller 更新

- [x] 1.1 **创建集群 Started** → `创建集群 {name}` ✅
- [x] 1.2 **创建集群 Finished** → `创建集群 {name}` ✅
- [x] 1.3 **移除独立的 Velero 安装事件** ✅
- [ ] 1.4 **编辑集群事件** → 待实现（需 Server 配合）
- [x] 1.5 **删除集群 Started** → `删除集群 {name}` ✅
- [x] 1.6 **删除集群 Finished** → 在 Finalizer 移除前发射 ✅

## 2. StorageRepository Controller 更新

- [x] 2.1 **创建存储 Started** → `创建存储 {name}` ✅
- [x] 2.2 **创建存储 Finished/Failed** → `创建存储 {name}` ✅
- [ ] 2.3 **编辑存储事件** → 待实现（需 Server 配合）
- [x] 2.4 **删除存储 Started** → `删除存储 {name}` ✅
- [x] 2.5 **删除存储 Finished** → 在 Finalizer 移除前发射 ✅

## 3. AppBackup Controller 更新

- [x] 3.1 **创建应用备份 Started/Finished** → `创建应用备份 {name}` ✅
- [x] 3.2 **执行备份 Started** → `应用备份 {name} 执行备份 {backupName}` ✅
- [x] 3.3 **重试备份 Started** → `应用备份 {name} 重试备份 {backupName}` ✅
- [x] 3.4 **执行备份 Finished** → `应用备份 {name} 执行备份 {backupName}` ✅
- [x] 3.5 **取消备份事件** → `应用备份 {name} 取消备份 {backupName}` ✅
- [x] 3.6 **删除备份历史事件** → `应用备份 {name} 删除备份 {backupName}` ✅
- [x] 3.7 **删除应用备份 Started** → `删除应用备份 {name}` ✅
- [x] 3.8 **删除应用备份 Finished** → 在 Finalizer 移除前发射 ✅

## 4. AppRestore Controller 更新

- [x] 4.1 **创建应用恢复 Started/Finished** → `创建应用恢复 {name}` ✅
- [x] 4.2 **执行恢复 Started** → `应用恢复 {name} 执行恢复 {restoreName}` ✅
- [x] 4.3 **执行恢复 Finished** → `应用恢复 {name} 执行恢复 {restoreName}` ✅
- [x] 4.4 **取消恢复事件** → `应用恢复 {name} 取消恢复` ✅
- [x] 4.5 **重试恢复事件** → `应用恢复 {name} 重试恢复` ✅
- [x] 4.6 **删除应用恢复 Started/Finished** → `删除应用恢复 {name}` ✅

## 5. 测试验证

- [x] 5.1 编译验证：确保 Operator 编译通过 ✅
- [ ] 5.2 集群生命周期测试 → 待手动验证
- [ ] 5.3 存储生命周期测试 → 待手动验证
- [ ] 5.4 应用备份生命周期测试 → 待手动验证
- [ ] 5.5 应用恢复生命周期测试 → 待手动验证
- [ ] 5.6 API 验证 → 待手动验证

## 6. 文档更新

- [ ] 6.1 更新 `openspec/specs/development-standards/spec.md` 中的事件格式规范
- [ ] 6.2 创建全局事件目录文档 `openspec/specs/global-events/spec.md`

---

## 进度统计

| 模块 | 已完成 | 待完成 | 完成率 |
|------|--------|--------|--------|
| Cluster | 5/6 | 1 (编辑) | 83% |
| Storage | 4/5 | 1 (编辑) | 80% |
| AppBackup | 8/8 | 0 | 100% |
| AppRestore | 6/6 | 0 | 100% |
| **总计** | **23/25** | **2** | **92%** |

---

## 修改的文件列表

```
internal/controller/
├── cluster_controller.go               # ✅ 创建/删除集群事件
├── storagerepository_controller.go     # ✅ 创建/删除存储事件
├── global_event_test.go                # ✅ BDD 测试
├── appbackup/
│   ├── appbackup_pending.go            # ✅ 创建应用备份事件
│   ├── appbackup_ready.go              # ✅ 执行/重试/取消/删除备份事件
│   └── appbackup_state.go              # ✅ 删除应用备份事件
└── apprestore/
    ├── apprestore_state.go             # ✅ 创建/删除应用恢复事件
    ├── apprestore_restoring.go         # ✅ 执行恢复事件
    └── apprestore_controller.go        # ✅ 取消/重试恢复事件
```
