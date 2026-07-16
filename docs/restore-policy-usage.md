# DisasterInstance 恢复策略使用说明

本文汇总当前已验证的 `DisasterInstance.spec.restorePolicy`、`spec.veleroHooks` 和 DataSync 行为。目标是给实施和排障时提供稳定口径，避免再从多个 OpenSpec proposal、测试计划和历史决策中拼接规则。

## 1. 能力边界

当前恢复策略主要覆盖三类需求：

1. Velero Hook 透传：把 Velero 原生 backup/restore hook 下发到 DataSync 生成的 AppBackup/AppRestore。
2. 资源修改器：通过 `restorePolicy.modifierRules` 或 `bulkModifierActions` 在恢复阶段改写资源字段。
3. DataSync 数据恢复优化：源端无可恢复 PVC 时跳过空数据同步；有 PVC 时继续走 Velero FSB 数据恢复。

ResourceSync 负责同步 Kubernetes 资源骨架，不执行实例级 Velero Hook。DataSync 负责 Pod/PVC/PV 数据恢复，才会投影实例级 `veleroHooks.dataBackup` 和 `veleroHooks.dataRestore`。

## 2. Velero Hook 透传

### 2.1 配置入口

实例级 Hook 写在 `DisasterInstance.spec.veleroHooks`：

```yaml
spec:
  veleroHooks:
    dataBackup:
      resources:
        - name: quiesce-app
          includedNamespaces:
            - blueking
          includedResources:
            - pods
          labelSelector:
            matchLabels:
              app: bk-demo
          pre:
            - exec:
                container: app
                command:
                  - /bin/sh
                  - -c
                  - /opt/hooks/pre-backup.sh
                timeout: 5m
          post:
            - exec:
                container: app
                command:
                  - /bin/sh
                  - -c
                  - /opt/hooks/post-backup.sh
                timeout: 5m
    dataRestore:
      resources:
        - name: restore-init-app
          includedNamespaces:
            - blueking
          includedResources:
            - pods
          labelSelector:
            matchLabels:
              app: bk-demo
          postHooks:
            - init:
                initContainers:
                  - name: restore-init-bk-demo
                    image: busybox:1.36
                    command:
                      - /bin/sh
                      - -c
                      - /opt/hooks/after-restore.sh
```

投影规则：

| 入口 | 下游对象 | 行为 |
| --- | --- | --- |
| `veleroHooks.dataBackup` | DataSync 生成的 `ds-*` AppBackup | 写入 `AppBackup.spec.template.hooks` |
| `veleroHooks.dataRestore` | DataSync 生成的 data AppRestore | 写入 `AppRestore.spec.template.hooks` |
| `DisasterDrill.spec.veleroHooks.dataRestore` | Drill data restore | 覆盖实例级 `dataRestore` |
| ResourceSync | 无 | 不投影 Hook |

Hook 参数保持 Velero 原生透传。平台不渲染 `${...}`、`{{ ... }}` 这类占位符，也不会把 namespace、cluster 或 secret 自动注入命令。敏感值应通过 `envFrom.secretRef`、`env.valueFrom.secretKeyRef` 或挂载文件传递，不应写进 `command`、`args`。

### 2.2 labelSelector 的 OR 写法

Velero 使用 Kubernetes `metav1.LabelSelector`。`matchLabels` 是 AND 语义，不能写两个相同 key：

```yaml
labelSelector:
  matchLabels:
    app: abc
    app: qwe
```

这种写法在 YAML/JSON 层也不成立，后一个 `app` 会覆盖前一个。若要表达 `app=abc OR app=qwe`，使用 `matchExpressions` 的 `In`：

```yaml
labelSelector:
  matchExpressions:
    - key: app
      operator: In
      values:
        - abc
        - qwe
```

JSON 写法：

```json
{
  "labelSelector": {
    "matchExpressions": [
      {
        "key": "app",
        "operator": "In",
        "values": ["abc", "qwe"]
      }
    ]
  }
}
```

如果是不同 key 的 OR，例如 `app=abc OR component=qwe`，单个 Kubernetes LabelSelector 不能表达。此时拆成多个 Hook resource 条目，每条使用自己的 selector：

```yaml
resources:
  - name: restore-hook-app-abc
    includedResources: ["pods"]
    labelSelector:
      matchLabels:
        app: abc
    postHooks:
      - exec:
          container: app
          command: ["/bin/sh", "-c", "/opt/hooks/restore.sh"]
  - name: restore-hook-component-qwe
    includedResources: ["pods"]
    labelSelector:
      matchLabels:
        component: qwe
    postHooks:
      - exec:
          container: app
          command: ["/bin/sh", "-c", "/opt/hooks/restore.sh"]
```

### 2.3 多个 restore init hook 的容器名称

多个 restore init hook 可以配置多条 `postHooks[].init.initContainers[]`，但同一个被恢复的 Pod 最终看到的 initContainer 名称必须唯一。

建议：

1. 不要让多个会命中同一个 Pod 的 init hook 使用相同 `initContainers[].name`。
2. 如果两个 Hook selector 互斥，技术上每个 Pod 只会收到其中一个 initContainer；但为了后续维护，仍建议全局使用唯一名称。
3. 平台当前按 Velero 原生结构透传，不自动重命名 initContainer。

推荐命名：

```yaml
postHooks:
  - init:
      initContainers:
        - name: restore-init-db-migrate
          image: busybox:1.36
          command: ["/bin/sh", "-c", "/hooks/db-migrate.sh"]
  - init:
      initContainers:
        - name: restore-init-cache-warmup
          image: busybox:1.36
          command: ["/bin/sh", "-c", "/hooks/cache-warmup.sh"]
```

不要这样配置会命中同一个 Pod 的多个 init hook：

```yaml
postHooks:
  - init:
      initContainers:
        - name: restore-init
          image: busybox:1.36
  - init:
      initContainers:
        - name: restore-init
          image: busybox:1.36
```

原因是 Kubernetes Pod 中容器名称必须唯一；重复名称会导致恢复出的 Pod 校验失败。

### 2.4 DataSync Trafficless 与 restore exec hook

DataSync 的数据恢复会通过 trafficless Pod 承接 FSB 数据回填。由于 trafficless 过程中业务 labels 可能被改写，平台会对 data restore 的 exec hook selector 做 marker label 适配，保证 exec hook 仍能命中恢复出的临时 Pod。

注意：

1. restore init hook 保持原 selector，不做 marker 改写。
2. restore exec hook 可能被拆分成独立 Hook resource，并使用 `testudo.softcdata.com/data-restore-hook-<index>=true` 这类 marker selector。
3. ResourceSync 不投影 restore hook，因此资源恢复阶段不会执行实例级 Hook。

## 3. DataSync PVC 行为

### 3.1 无 PVC 时自动跳过数据恢复

DataSync 在一次新的同步触发后、创建或触发 AppBackup 前，会检查源端保护范围内是否存在可恢复 PVC。

无可恢复 PVC 时：

1. DataSync 直接以成功 no-op 收敛为 `Ready`。
2. 不创建或触发 AppBackup、Velero Backup、AppRestore、Velero Restore。
3. 不创建 trafficless Pod。
4. 不要求 StorageRepository 当前可用。
5. 同步历史记录为 `Skipped`，统计按 completed 处理。

可恢复 PVC 的判断规则：

1. 未配置实例 `labelSelector`：实例命名空间中任意非删除中的 PVC 都视为可恢复。
2. 配置了实例 `labelSelector`：PVC 自身匹配 selector，或匹配 selector 的 Pod 引用了该 PVC，都视为可恢复。
3. 源集群 list Pod/PVC 失败时，DataSync 失败关闭，不伪装成无 PVC 跳过。

### 3.2 单独跳过某些 PVC

无 PVC no-op 只处理“保护范围内没有任何可恢复 PVC”的场景。若命名空间中有多个 PVC，只想跳过其中一部分，需要从保护范围或 Velero 原生行为处理。

优先级建议：

1. 让实例 `labelSelector` 只命中需要保护的工作负载，避免不需要同步的 PVC 被匹配 Pod 间接纳入。
2. 对不需要备份的资源使用 Velero 原生排除注解。
3. 对 Pod 级文件系统备份，使用 Velero 的 volume exclude 注解排除具体 volume。

示例：跳过两个 PVC 资源本身：

```bash
kubectl -n blueking annotate pvc bk-job-pv-claim-job-local \
  velero.io/exclude-from-backup=true --overwrite

kubectl -n blueking annotate pvc license-pvc-claim \
  velero.io/exclude-from-backup=true --overwrite
```

如果 PVC 被某个 Pod volume 引用，并且需要跳过 FSB 数据备份，还需要在 Pod 模板上排除对应 volume 名称。例如 Deployment：

```bash
kubectl -n blueking patch deployment <deployment-name> --type merge -p '{
  "spec": {
    "template": {
      "metadata": {
        "annotations": {
          "backup.velero.io/backup-volumes-excludes": "job-local,license"
        }
      }
    }
  }
}'
```

这里的 `job-local,license` 是 Pod `spec.volumes[].name`，不是 PVC 名称。修改后需要确认新 Pod 模板已带上注解。

### 3.3 DataSync 恢复时改写 PVC storageClassName

对静态 PV/NFS、不同集群 StorageClass 名称不一致等场景，可以用 `restorePolicy.modifierRules` 对 PVC 的 `spec.storageClassName` 做可逆 pair 改写。

下面是有效 JSON 示例，可放入实例恢复策略的 modifier rules 输入中：

```json
[
  {
    "id": "rewrite-skywalking-pvc-sc",
    "mode": "reversible",
    "applyTo": ["dataSync"],
    "priority": 100,
    "conditions": {
      "groupResource": "persistentvolumeclaims",
      "namespaces": ["blueking"],
      "resourceNameRegex": "^bk-skywalking-agent-nfs-pvc$"
    },
    "pair": {
      "path": "/spec/storageClassName",
      "sourceValue": "local-storage",
      "targetValue": "nfs-client"
    }
  },
  {
    "id": "rewrite-bkrepo-pvc-sc",
    "mode": "reversible",
    "applyTo": ["dataSync"],
    "priority": 100,
    "conditions": {
      "groupResource": "persistentvolumeclaims",
      "namespaces": ["blueking"],
      "resourceNameRegex": "^bk-repo-bkrepo-nfs-pvc$"
    },
    "pair": {
      "path": "/spec/storageClassName",
      "sourceValue": "bkrepo-nfs",
      "targetValue": "nfs-client"
    }
  }
]
```

注意：

1. JSON 顶层是数组。
2. 每个对象之间用逗号分隔，最后一个对象后面不能有逗号。
3. `resourceNameRegex` 建议使用 `^...$` 精确匹配，避免误改其他 PVC。
4. `reversible` 的正式结构只使用 `pair.path/sourceValue/targetValue`，不要再使用旧的 `transform`。

## 4. 批量修改器与动态镜像重写

### 4.1 何时用 rewriteImage

如果只是替换固定字符串，可以继续使用 `replaceExactValue`。但镜像值会随发布变化，绑定完整镜像容易失效：

```text
10.134.81.9:5000/blueking/app:v1.0.0
10.134.81.9:5000/blueking/app:v1.0.1
10.134.81.9:5000/blueking/app@sha256:...
```

镜像仓库前缀迁移应使用 `bulkModifierActions[].action=rewriteImage`。它只声明源/目标前缀，恢复构建时实时读取源集群当前镜像并生成本次 AppRestore 的 pair 规则。

`rewriteImage` 当前已验证作用于：

1. ResourceSync 资源恢复。
2. Drill 资源恢复。

它不会把当前完整镜像值回写到用户原始 DSL，也不要求持久化 `modifierRuleSnapshot` / `modifierRuleSnapshotHash`。

### 4.2 示例

```json
[
  {
    "id": "rewrite-primary-registry",
    "action": "rewriteImage",
    "enabled": true,
    "applyTo": ["resourceSync", "drill"],
    "directionPolicy": "Auto",
    "imageRewrite": {
      "sourcePrefix": "10.134.81.9:5000/",
      "targetPrefix": "registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/",
      "unmatchedPolicy": "Keep",
      "digestPolicy": "Preserve"
    }
  }
]
```

源镜像：

```text
10.134.81.9:5000/groundnuty/k8s-wait-for:v1.5.1
```

目标镜像：

```text
registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/groundnuty/k8s-wait-for:v1.5.1
```

digest 镜像在 `digestPolicy=Preserve` 下会保留 digest suffix：

```text
10.134.81.9:5000/blueking/app@sha256:abcdef
```

会改写为：

```text
registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/blueking/app@sha256:abcdef
```

### 4.3 initContainers 覆盖范围

动态镜像重写会扫描工作负载 PodSpec 中的镜像字段，包括：

1. `containers[].image`
2. `initContainers[].image`
3. `ephemeralContainers[].image`

已覆盖的资源类型：

| 资源 | groupResource | 路径示例 |
| --- | --- | --- |
| Deployment | `deployments.apps` | `/spec/template/spec/initContainers/0/image` |
| StatefulSet | `statefulsets.apps` | `/spec/template/spec/initContainers/0/image` |
| DaemonSet | `daemonsets.apps` | `/spec/template/spec/initContainers/0/image` |
| ReplicaSet | `replicasets.apps` | `/spec/template/spec/initContainers/0/image` |
| ReplicationController | `replicationcontrollers` | `/spec/template/spec/initContainers/0/image` |
| Job | `jobs.batch` | `/spec/template/spec/initContainers/0/image` |
| CronJob | `cronjobs.batch` | `/spec/jobTemplate/spec/template/spec/initContainers/0/image` |
| Pod | `pods` | `/spec/initContainers/0/image` |

例如下面两个 initContainers 都会被改写：

```yaml
initContainers:
  - name: wait-storages
    image: 10.134.81.9:5000/groundnuty/k8s-wait-for:v1.5.1
  - name: bk-apigateway-operator
    image: 10.134.81.9:5000/groundnuty/k8s-wait-for:v1.5.1
```

生成规则会分别命中：

```text
/spec/template/spec/initContainers/0/image
/spec/template/spec/initContainers/1/image
```

### 4.4 未匹配策略

`unmatchedPolicy` 支持：

| 值 | 行为 |
| --- | --- |
| `Keep` | 未命中镜像保持原值，并在摘要中记录未匹配数量 |
| `Fail` | 只要保护范围内存在未命中镜像，恢复构建失败关闭，并返回未匹配明细 |

生产环境建议先用 `Keep` 完成迁移验证，再按镜像治理要求切换为 `Fail`。

### 4.5 与旧 imageSources/imageRewrite 的关系

旧的 `Cluster.spec.imageSources` + `DisasterConfig.spec.imageRewrite` 是集群级镜像源映射模型，不再作为新实例的推荐路径。

新的推荐路径是实例级：

```text
DisasterInstance.spec.restorePolicy.bulkModifierActions[].action = rewriteImage
```

迁移原则：

1. 旧 `sourcePrefix/targetPrefix` 可以直接迁移到 `imageRewrite.sourcePrefix/targetPrefix`。
2. 原来用完整镜像做 `replaceExactValue` 的规则，应迁移为前缀级 `rewriteImage`。
3. 若必须对单个镜像做固定替换，仍可保留 `replaceExactValue`，但需要接受镜像 tag/digest 变化后规则失效的维护成本。

## 5. 排障命令

检查实例策略：

```bash
kubectl -n disaster-system get disasterinstance <name> -o yaml
```

检查 DataSync 是否因无 PVC 跳过：

```bash
kubectl -n disaster-system get datasync <name> -o jsonpath='{.status.state}{"\n"}{.status.reason}{"\n"}{.status.message}{"\n"}'
kubectl -n disaster-system get datasync <name> -o json | jq '.status.history[-5:]'
```

检查 AppRestore 是否带上资源修改器：

```bash
kubectl -n disaster-system get apprestore <name> -o json | jq '.spec.resourceModifierRules'
```

检查目标集群 Velero Restore：

```bash
kubectl -n velero get restore <restore-name> -o yaml
```

检查动态镜像重写是否命中 initContainers：

```bash
kubectl -n disaster-system get apprestore <name> -o json \
  | jq '.spec.resourceModifierRules[]? | select(.id | test("runtime-image-rewrite")) | {id, conditions, pair}'
```

检查 Hook 状态：

```bash
kubectl -n disaster-system get datasync <name> -o json \
  | jq '.status.history[-5:][] | {status, backupName, restoreName, backupHookStatus, restoreHookStatus}'
```

检查 Velero Backup/Restore 原生 hookStatus：

```bash
kubectl -n velero get backup <backup-name> -o json | jq '.status.hookStatus'
kubectl -n velero get restore <restore-name> -o json | jq '.status.hookStatus'
```
