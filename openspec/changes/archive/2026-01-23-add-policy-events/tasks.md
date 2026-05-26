# 任务清单：为策略添加全局事件上报

## 1. 数据结构变更

### 1.1 更新 DisasterPolicyStatus
- [x] 1.1.1 在 `pkg/apis/disaster/v1/disasterpolicy_types.go` 中添加字段：
  - `ObservedGeneration int64`
  - `LastEventPhase string`
  - `LastState PolicyState`
- [x] 1.1.2 运行 `make generate manifests` 更新生成代码和 CRD

## 2. Controller 事件发射

### 2.1 创建策略事件
- [x] 2.1.1 在 Finalizer 添加后发射 "创建策略 Started" 事件（已包含在重构逻辑中，通过 Generation 判断）
- [x] 2.1.2 在首次 Reconcile 成功后（`ObservedGeneration == 0`）发射 "创建策略 Finished" 事件

### 2.2 编辑/启用/禁用策略事件
- [x] 2.2.1 检测 `Generation > ObservedGeneration && ObservedGeneration > 0`
- [x] 2.2.2 比较 `Spec.State` 与 `Status.LastState`：
  - Disabled → Enabled: 发射 "启用策略"
  - Enabled → Disabled: 发射 "禁用策略"
  - 其他情况: 发射 "编辑策略"
- [x] 2.2.3 更新 `Status.LastState = Spec.State`
- [x] 2.2.4 更新 `Status.ObservedGeneration = Generation`

### 2.3 删除策略事件
- [x] 2.3.1 在 `handleDelete` 开始时发射 "删除策略 Started" 事件（仅一次，使用 LastEventPhase 防抖）
- [x] 2.3.2 在移除 Finalizer 前发射 "删除策略 Finished" 事件

## 3. 更新全局事件规范

- [x] 3.1 在 `openspec/specs/global-events/spec.md` 中添加 Policy 事件目录（已通过 Spec Delta 完成）

## 4. 测试验证

- [x] 4.1 创建策略，验证 Started/Finished 事件
- [x] 4.2 编辑策略，验证编辑事件
- [x] 4.3 启用/禁用策略，验证对应事件
- [x] 4.4 删除策略，验证删除事件
- [x] 4.5 通过 kubectl 直接修改策略，验证事件发射

## 验证命令

```bash
# 更新 CRD
kubectl apply -f config/crd/bases/testudo.softcdata.com_disasterpolicies.yaml

# 运行 Operator
make run

# 创建策略后查看事件
kubectl get events -A -l testudo.softcdata.com/task-event=true | grep 策略
```
