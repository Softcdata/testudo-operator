# E2E 测试流程步骤：V2 结构化事件发射覆盖

## 1. 文档目的

本流程用于在真实环境验证 V2 六大模块的结构化事件是否满足以下要求：
- 事件生命周期完整（Started/Progress/Finished）
- 事件载荷是 JSON 且字段齐全
- failover 后 reprotect 与反向 sync 的方向语义正确
- 删除路径事件不丢失（先 Finished 再 Finalizer）

## 2. 测试前置条件

1. 管理集群可访问，`disaster-operator` 与 `disaster-server` 均已重启且健康。
2. 已存在可用集群 CR（示例：`c170`、`c171`），并可正常连通。
3. `StorageRepository` 可用，`DisasterConfig` 与策略可创建。
4. 本机安装：`kubectl`、`jq`、`curl`（可选 `wscat`）。
5. 使用同一命名空间（以下示例使用 `disaster-system`）。

## 3. 统一变量（建议先执行）

```bash
export NS=disaster-system
export DI=event-e2e-di
export DC=event-e2e-dc
export DG=event-e2e-dg
export DRILL=event-e2e-drill
export OP_FAILOVER=event-e2e-failover
export OP_REPROTECT=event-e2e-reprotect
export OP_SYNC=event-e2e-syncresource
export SOURCE_CLUSTER=c170
export TARGET_CLUSTER=c171
```

## 4. 事件观测命令（全流程复用）

### 4.1 看结构化事件总览

```bash
kubectl get events -A -l testudo.softcdata.com/task-event=true --sort-by=.lastTimestamp
```

### 4.2 按资源过滤并解析 JSON 消息体

```bash
kubectl get events -n ${NS} -l testudo.softcdata.com/task-event=true -o json \
| jq -r '.items[]
  | select(.involvedObject.kind=="DisasterOperation" and .involvedObject.name=="'"${OP_FAILOVER}"'")
  | [.lastTimestamp,.reason,.type,
     ((.message | (try fromjson catch {"task":"<invalid-json>","status":"","message":.}))|.task),
     ((.message | (try fromjson catch {"task":"","status":"","message":.}))|.status),
     ((.message | (try fromjson catch {"task":"","status":"","message":.}))|.message)]
  | @tsv'
```

### 4.3 Server 聚合验证（历史）

```bash
curl -s "http://<disaster-server>/apis/v1/events?namespace=${NS}" | jq '.'
```

### 4.4 Server 实时流验证（watch）

```bash
# 示例（若环境提供 WebSocket）
wscat -c "ws://<disaster-server>/apis/v1/watch/events?namespace=${NS}"
```

## 5. 步骤 A：准备基础对象（DisasterInstance/DataSync/ResourceSync）

### 5.1 创建 DisasterConfig 与 DisasterInstance

```yaml
apiVersion: testudo.softcdata.com/v1
kind: DisasterConfig
metadata:
  name: event-e2e-dc
spec:
  sourceCluster: c170
  targetCluster: c171
  storageRepository: <your-storage-repository>
  dataSyncPolicy: <your-datasync-policy>
  resourceSyncPolicy: <your-resourcesync-policy>
  dataSyncType: fsb
---
apiVersion: testudo.softcdata.com/v1
kind: DisasterInstance
metadata:
  name: event-e2e-di
  namespace: disaster-system
spec:
  config: event-e2e-dc
  namespaces:
  - <protected-namespace>
```

执行：

```bash
kubectl apply -f <file>.yaml
kubectl get di -n ${NS} ${DI} -o wide
```

### 5.2 验证点

1. `DisasterInstance` 初始化阶段存在 Started/Progress/Finished 事件。
2. 自动创建的 `DataSync`、`ResourceSync` 在首次同步时有结构化事件。
3. 任意结构化事件 `message` 可被 `jq fromjson` 正常解析，且包含 `task/status/message`。

## 6. 步骤 B：验证 DisasterOperation（failover）

### 6.1 创建 failover 操作

```yaml
apiVersion: testudo.softcdata.com/v1
kind: DisasterOperation
metadata:
  name: event-e2e-failover
  namespace: disaster-system
spec:
  instanceName: event-e2e-di
  operationType: failover
  timeoutMinutes: 30
```

执行：

```bash
kubectl apply -f <failover-op>.yaml
kubectl get disasteroperation -n ${NS} ${OP_FAILOVER} -w
```

### 6.2 验证点

1. 出现 `ExecutionStarted`。
2. 至少出现以下步骤的 `ExecutionProgress`（允许实现细节不同，但应覆盖关键阶段）：
   - `PreCheck`
   - `FinalSync`
   - `ScaleDownSource`
   - `ScaleUpTarget`
   - `SwitchRoles`
3. 终态必须出现 `ExecutionFinished`。
4. 若失败，`ExecutionFinished` 应为 `Warning` 且状态为 `Failed`。

## 7. 步骤 C：验证反向保护与反向同步语义（核心）

### 7.1 创建 reprotect 操作

```yaml
apiVersion: testudo.softcdata.com/v1
kind: DisasterOperation
metadata:
  name: event-e2e-reprotect
  namespace: disaster-system
spec:
  instanceName: event-e2e-di
  operationType: reprotect
```

执行：

```bash
kubectl apply -f <reprotect-op>.yaml
kubectl get disasteroperation -n ${NS} ${OP_REPROTECT} -w
```

### 7.2 创建 syncresource 操作（反向同步）

```yaml
apiVersion: testudo.softcdata.com/v1
kind: DisasterOperation
metadata:
  name: event-e2e-syncresource
  namespace: disaster-system
spec:
  instanceName: event-e2e-di
  operationType: syncresource
```

执行：

```bash
kubectl apply -f <syncresource-op>.yaml
kubectl get disasteroperation -n ${NS} ${OP_SYNC} -w
```

### 7.3 验证点（必须通过）

1. `kubectl get di -n ${NS} ${DI} -o jsonpath='{.status.primaryCluster}{"->"}{.status.secondaryCluster}{"\n"}'` 显示角色已切换到运行期新方向。
2. `reprotect` 与 `syncresource` 的事件消息中 source/target 语义与当前 `primary->secondary` 一致。
3. 不出现“仍使用静态 sourceCluster/targetCluster 方向”的反向描述。

## 8. 步骤 D：验证 DisasterGroup 事件

### 8.1 创建组对象（示例单层）

```yaml
apiVersion: testudo.softcdata.com/v1
kind: DisasterGroup
metadata:
  name: event-e2e-dg
  namespace: disaster-system
spec:
  levels:
  - ["event-e2e-di"]
  policy:
    failPolicy: Stop
    timeoutMin: 30
```

执行：

```bash
kubectl apply -f <group>.yaml
```

### 8.2 触发组级操作（示例 failover）

```yaml
apiVersion: testudo.softcdata.com/v1
kind: DisasterOperation
metadata:
  name: event-e2e-group-failover
  namespace: disaster-system
spec:
  groupName: event-e2e-dg
  operationType: failover
```

验证点：
1. 有组级 Started。
2. 按 Level 推进有 Progress。
3. 终态有 Finished。

## 9. 步骤 E：验证 DisasterDrill 事件

### 9.1 创建演练对象

```yaml
apiVersion: testudo.softcdata.com/v1
kind: DisasterDrill
metadata:
  name: event-e2e-drill
  namespace: disaster-system
spec:
  instanceName: event-e2e-di
  targetCluster: c171
  waitUntilReady: true
  confirmed: false
```

执行：

```bash
kubectl apply -f <drill>.yaml
kubectl get disasterdrill -n ${NS} ${DRILL} -w
```

### 9.2 确认执行与清理

```bash
kubectl patch disasterdrill -n ${NS} ${DRILL} --type merge -p '{"spec":{"confirmed":true}}'
kubectl patch disasterdrill -n ${NS} ${DRILL} --type merge -p '{"spec":{"cleanup":true}}'
```

验证点：
1. Ready->Executing 有 Started。
2. 执行步骤有 Progress。
3. Completed/Failed/CleanedUp 均有 Finished。

## 10. 步骤 F：删除路径事件持久化验证

1. 删除 `DisasterInstance`（或 Drill/Group）：

```bash
kubectl delete di -n ${NS} ${DI}
```

2. 观察该资源最终事件流，确认：
   - 删除流程存在 `ExecutionFinished`
   - 事件时间戳早于 Finalizer 被移除导致对象消失的时间点

## 11. 失败判定标准

出现任一项即判定本轮 E2E 不通过：
1. Started/Finished 未成对出现（非中断测试前提下）。
2. 结构化事件 `message` 非法 JSON，或缺少 `task/status/message`。
3. failover 后 reprotect/syncresource 事件方向与运行期角色不一致。
4. 删除路径未观察到 Finished，或发生先删 Finalizer 后发事件。

## 12. 测试后清理

```bash
kubectl delete disasterdrill -n ${NS} ${DRILL} --ignore-not-found
kubectl delete disasteroperation -n ${NS} ${OP_FAILOVER} ${OP_REPROTECT} ${OP_SYNC} --ignore-not-found
kubectl delete disastergroup -n ${NS} ${DG} --ignore-not-found
kubectl delete disasterinstance -n ${NS} ${DI} --ignore-not-found
kubectl delete disasterconfig ${DC} --ignore-not-found
```

如需保留现场用于排障，可跳过清理并导出事件：

```bash
kubectl get events -n ${NS} -l testudo.softcdata.com/task-event=true -o json > v2-events-e2e.json
```
