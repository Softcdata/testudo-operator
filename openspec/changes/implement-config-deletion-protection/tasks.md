---
name: implement-config-deletion-protection
---

# 实施任务

## 任务列表

- [ ] Task 1: 扫描代码，确认 DisasterConfig Controller 实现现状
- [ ] Task 2: 修改 DisasterConfig Controller 添加 Finalizer 逻辑
- [ ] Task 3: 实现引用检查逻辑 (List DisasterInstances)
- [ ] Task 4: 验证删除保护 (创建 Config -> 创建引用 -> 尝试删除 -> 验证失败 -> 删除引用 -> 验证删除成功)

---

## Task 1: 扫描代码
确认 `internal/controller/disasterconfig_controller.go` 是否已有 Finalizer 常量或其他逻辑。

## Task 2: 添加 Finalizer
- 常量: `ConfigFinalizer = "testudo.softcdata.com/config-finalizer"`
- 逻辑: `controllerutil.AddFinalizer`

## Task 3: 引用检查
- 使用 `List` 方法获取 DisasterInstance。
- 过滤 `Spec.Config == config.Name`。
- 如果存在，Record Event 并 Log Warning。

## Task 4: 验证
手动测试或单元测试。
