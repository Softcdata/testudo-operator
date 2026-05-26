# Specification: Group Instance Management & Integrity

## MODIFIED Requirements 

### Requirement: 容灾组列表数据及成员列表 MUST 不静默忽略缺失实例
- `disaster-server` 在调用 `GET /groups/:name/instances` (或获取容灾组信息的其余接口) 时，若容灾组所声明包含的 `DisasterInstance` 在 K8s 集群中无法被查询到（`errors.IsNotFound`），服务器 `MUST NOT` 将其剔除或略过。
- 代替的，服务器 `MUST` 返回一个带有原有实例名称，且将其 `FsmState` 标识为 `NotFound` 的结构体，以便客户端识别并处理“幽灵引用”。

#### Scenario: 用户获取包含被删实例所在组的容灾组实例列表
- **When** 用户调用 `GET /groups/DG1/instances` 接口
- **And** 容灾组 `DG1` 包含 `Inst-A`，且 `Inst-A` 被手动删除（脱离约束）。
- **Then** 接口返回 `200 OK`，响应中的数组数据 `MUST` 包含 `Name: "Inst-A"`，并且 `FsmState: "NotFound"`。

## ADDED Requirements

### Requirement: 系统 MUST 防止受容灾组管理的实例被意外删除
- `DisasterInstance` 的 `delete` 操作 `MUST` 被拦截器/控制器阻止，当且仅当存在任意一个 `DisasterGroup` 的 `Spec.Levels` 引用的实例名称中包含该 `DisasterInstance`。
- 如果存在引用，控制器 `MUST` 派发一条原因标识为 `DeletionBlocked`，类型为 `Warning` 的事件：`无法删除实例 %s：该实例正被容灾组 %s (位于 Level %d) 引用于保护。请先将其从此容灾组中移除，或添加 testudo.softcdata.com/force-delete=true 强制删除。`，并重新放回队列（维持现状不移除 finalizer）。

#### Scenario: 用户尝试删除被引用的实例
- **Given** 用户在一个容灾组 `DG1` 中加入了 `Inst-A`。
- **When** 用户通过 kubectl 尝试删除该实例 `kubectl delete di Inst-A`
- **Then** 控制器截断删除流程，在 `Inst-A` 会输出 `Warning` 表明它正被 `DG1` 所使用，除非强制。

### Requirement: 系统 MUST 提供容灾组实例编辑与说明标签更新的接口
- `disaster-server` `MUST` 提供 `PUT` 或 `PATCH` 到 `/groups/:name` 的端点。
- 调用该接口时，传入的 `levels` 二维数组 `MUST` 完全覆盖和更新 `DisasterGroup.Spec.Levels`（从而实现容灾库的添加、删除、层级调整）。
- 调用该接口时，传入的 `description` 字符串 `MUST` 同步更新容灾组自身的 `testudo.softcdata.com/description` 标签信息。

#### Scenario: 用户编辑修复含有损坏实例的容灾组
- **Given** 容灾组 `DG1` 包含一个幽灵实例 `Broken-Inst`（接口显示为 NotFound）。
- **When** 用户通过客户端调用 `PUT /groups/DG1` 接口并传入不再包含 `Broken-Inst` 的全新 `levels` 数组以及新的 `description` 描述。
- **Then** API 返回成功的相应，且集群中 `DG1` CRD 成功去除了这个幽灵节点，完成状态自愈。

### Requirement: 容灾组执行编排容灾操作时 MUST 不因幽灵实例宕机
- `DisasterOperation` 在执行处理 `Group` 层级阶段展开为单个实例并获取实例时，若无法找到子实例（`errors.IsNotFound`），它 `MUST` 妥善处理缺失实例：
  - 如果 `FailPolicy` 置为 `Stop`，则直接中断整个组级别的灾难恢复/演练，且 `MUST` 更新父 Operation状态为 `Failed` 并提供明文理由 `DisasterInstance <name> 未找到`。
  - 如果 `FailPolicy` 等于 `Continue`，则 `MUST` 忽略此实例错误，`MUST` 记录该实例执行进度为 `NotFound` (甚至 `Failed` 视具体逻辑)，并继续放行到下一个组实例。

#### Scenario: 执行一个具有损坏实例且策略为继续的组级别故障切换
- **Given** 容灾组 `DG2` 包含实例 `Broken-Inst`（因外力删除）以及一台正常实例 `Ok-Inst`。
- **And** `Policy.FailPolicy` 被设定为 `Continue`。
- **When** 用户通过 `POST /groups/DG2/actions` 触发布署演练或故障切换。
- **Then** `DisasterOperation` 遇到缺失的 `Broken-Inst` 时不会挂起整个任务。并在进度和消息中返回该子实例失败/找不到的明文信息，进而执行完毕 `Ok-Inst` 使外部组成功步入下一个级联/完成。
