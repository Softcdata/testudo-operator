# 设计：Velero Hook 透传接入

## 1. 总体模型

本设计只做 Velero 原生 Hook 透传。平台负责接收、保存、校验和投影 Hook 字段；执行、超时、失败语义由 Velero 控制。

输入面分三类：

| 场景 | 输入字段 | 投影目标 |
| --- | --- | --- |
| 手工备份 | `AppBackup` API `hooks` | `AppBackup.spec.template.hooks` -> Velero `Backup.spec.hooks` 或 `Schedule.spec.template.hooks` |
| 手工恢复 | `AppRestore` API `hooks` | `AppRestore.spec.template.hooks` -> Velero `Restore.spec.hooks` |
| 容灾实例 | `DisasterInstance.spec.veleroHooks.dataBackup` / `dataRestore` | DataSync 生成的 `AppBackup` / `AppRestore` |
| 演练覆盖 | `DisasterDrill.spec.veleroHooks.dataRestore` | Drill Data Restore 生成的 `AppRestore` |

## 2. 字段设计

### 2.1 手工 AppBackup

server 对外继续使用扁平 DTO：

```json
{
  "name": "backup-a",
  "cluster": "cluster-a",
  "includedNamespaces": ["app"],
  "hooks": {
    "resources": []
  }
}
```

`hooks` 类型与 Velero `velerov1.BackupHooks` 对齐，并写入 `AppBackup.spec.template.hooks`。Operator 创建 Velero `Backup` 和 `Schedule` 时已从 `AppBackup.spec.template` 复制，因此底层无需新增执行逻辑。

### 2.2 手工 AppRestore

server 对外新增 `hooks` 字段，类型与 Velero `velerov1.RestoreHooks` 对齐，并写入 `AppRestore.spec.template.hooks`。

`AppRestore` controller 当前在创建 Velero Restore 前会复制 `appRestore.Spec.Template` 并注入 `ResourceModifier`。实现时必须保持 `template.Hooks` 不被覆盖。

### 2.3 容灾实例

新增实例级字段：

```go
type DisasterVeleroHooks struct {
    // DataBackup applies only to DataSync-created AppBackup.
    DataBackup *velerov1.BackupHooks `json:"dataBackup,omitempty"`

    // DataRestore applies only to DataSync-created AppRestore.
    DataRestore *velerov1.RestoreHooks `json:"dataRestore,omitempty"`
}

type DisasterInstanceSpec struct {
    VeleroHooks *DisasterVeleroHooks `json:"veleroHooks,omitempty"`
}
```

投影规则：

- `dataBackup` 只投影到 DataSync 生成的 `AppBackup.spec.template.hooks`。
- `dataBackup` 不投影到 ResourceSync 生成的 `AppBackup`，因为 ResourceSync 当前排除 Pod/PVC/PV，Velero backup hook 没有稳定执行目标。
- `dataRestore` 只投影到 DataSync 生成的 `AppRestore.spec.template.hooks`。
- ResourceSync 资源恢复当前排除 Pod，因此不承诺执行 restore hook。

DataSync 控制器必须在既有 `ds-*` AppBackup 已存在时执行 desired spec/template 对齐。即使源/目标集群未变化，只要 `DisasterInstance.spec.veleroHooks.dataBackup` 变化，下一次同步前也必须更新既有 `AppBackup.spec.template.hooks`。实现可以对齐完整 desired template，也可以至少对 `Template.Hooks` 做差异更新，但测试必须覆盖“实例 hooks 更新后下一次备份使用新 hooks”。

### 2.4 演练覆盖

新增演练级字段：

```go
type DisasterDrillSpec struct {
    VeleroHooks *DisasterVeleroHooks `json:"veleroHooks,omitempty"`
}

type DrillConfig struct {
    VeleroHooks *DisasterVeleroHooks `json:"veleroHooks,omitempty"`
}
```

演练只使用 `veleroHooks.dataRestore`。当演练未提供 `dataRestore` 时，继承 `DisasterInstance.spec.veleroHooks.dataRestore`；当演练提供时，覆盖实例级 dataRestore。

`veleroHooks.dataBackup` 在演练请求中不生效，server 必须拒绝并返回明确错误，避免用户误以为演练会创建备份。DisasterDrillReconciler 创建或更新关联 DisasterOperation 时，必须把 `drill.spec.veleroHooks` 复制到 `operation.spec.drillConfig.veleroHooks`，否则执行端无法读取演练级覆盖。

### 2.5 同步历史 hookStatus 数据契约

容灾同步历史不能只依赖 Web 从多个资源临时拼装。Operator 必须扩展同步历史记录，保存备份和恢复两侧 Hook 汇总状态：

```go
type SyncHistoryHookStatus struct {
    HooksAttempted int `json:"hooksAttempted,omitempty"`
    HooksFailed    int `json:"hooksFailed,omitempty"`
}

type SyncHistoryRecord struct {
    BackupHookStatus  *SyncHistoryHookStatus `json:"backupHookStatus,omitempty"`
    RestoreHookStatus *SyncHistoryHookStatus `json:"restoreHookStatus,omitempty"`
}
```

DataSync 在记录同步历史时，应从关联 `AppBackup.status.history[].veleroStatus.hookStatus` 和 `AppRestore.status.restoreStatus.hookStatus` 复制 attempted/failed 计数。ResourceSync 如果没有投影 hooks，则这些字段保持空值。

server 同步历史 DTO 必须回显 `backupHookStatus` 和 `restoreHookStatus`，Web 只消费该稳定字段，不通过 BackupName/RestoreName 再查二级资源拼装。

## 3. Hook 传参设计

### 3.1 第一阶段不新增 params 抽象

Velero 原生 Hook schema 没有独立的 `params` 字段。本阶段坚持透传语义，因此平台不新增如下能力：

- 不提供 `params: {}` 字段。
- 不做 `${namespace}`、`${pod}`、`${cluster}` 等占位符替换。
- 不对 `exec.command` 做模板渲染。
- 不把平台上下文隐式注入到业务容器环境变量。

如果后续需要“填参数生成 hook”，应进入 HookTemplate 阶段，由模板渲染器把参数渲染成 Velero 原生 `hooks`，再复用本阶段的透传字段。

### 3.2 Backup exec hook 参数

Backup Hook 的参数只能通过 Velero 原生 `exec.command` 数组传递，或由命令读取业务容器中已经存在的环境变量、挂载文件、Secret、ConfigMap。

推荐形式是把可变参数拆成 argv，避免 shell 引号问题：

```json
{
  "hooks": {
    "resources": [
      {
        "name": "app-quiesce",
        "includedNamespaces": ["prod"],
        "includedResources": ["pods"],
        "labelSelector": {
          "matchLabels": {
            "app": "demo"
          }
        },
        "pre": [
          {
            "exec": {
              "container": "app",
              "command": [
                "/usr/local/bin/dr-hook",
                "pre-backup",
                "--mode",
                "quiesce"
              ],
              "onError": "Fail",
              "timeout": "30s"
            }
          }
        ],
        "post": [
          {
            "exec": {
              "container": "app",
              "command": [
                "/usr/local/bin/dr-hook",
                "post-backup",
                "--mode",
                "resume"
              ],
              "onError": "Continue",
              "timeout": "30s"
            }
          }
        ]
      }
    ]
  }
}
```

如果必须使用 shell，可以显式写成：

```json
{
  "exec": {
    "container": "app",
    "command": ["/bin/sh", "-c", "dr-hook pre-backup --mode=${DR_MODE:-quiesce}"],
    "onError": "Fail",
    "timeout": "30s"
  }
}
```

这里的 `${DR_MODE}` 是业务容器自身已有的环境变量，由 shell 在容器内解析；平台不做替换。

### 3.3 Restore exec hook 参数

Restore exec hook 与 Backup exec hook 一样，通过 `postHooks[].exec.command` 传参。它可以使用 `execTimeout`、`waitTimeout`、`waitForReady` 控制等待和执行行为：

```json
{
  "hooks": {
    "resources": [
      {
        "name": "app-after-restore",
        "includedNamespaces": ["prod"],
        "includedResources": ["pods"],
        "labelSelector": {
          "matchLabels": {
            "app": "demo"
          }
        },
        "postHooks": [
          {
            "exec": {
              "container": "app",
              "command": [
                "/usr/local/bin/dr-hook",
                "after-restore",
                "--target",
                "standby"
              ],
              "onError": "Fail",
              "waitForReady": true,
              "waitTimeout": "5m",
              "execTimeout": "1m"
            }
          }
        ]
      }
    ]
  }
}
```

### 3.4 Restore init hook 参数

Restore init hook 可以通过 Kubernetes init container 原生字段传参，包括 `args`、`env`、`envFrom`、`volumeMounts` 等。平台只透传 `initContainers`，不解析其中的业务参数。

```json
{
  "hooks": {
    "resources": [
      {
        "name": "app-init-restore",
        "includedNamespaces": ["prod"],
        "includedResources": ["pods"],
        "labelSelector": {
          "matchLabels": {
            "app": "demo"
          }
        },
        "postHooks": [
          {
            "init": {
              "initContainers": [
                {
                  "name": "restore-init",
                  "image": "registry.local/dr-tools:1.0",
                  "command": ["/bin/sh", "-c"],
                  "args": ["dr-restore-init --namespace=$POD_NAMESPACE"],
                  "env": [
                    {
                      "name": "POD_NAMESPACE",
                      "valueFrom": {
                        "fieldRef": {
                          "fieldPath": "metadata.namespace"
                        }
                      }
                    }
                  ],
                  "envFrom": [
                    {
                      "secretRef": {
                        "name": "app-dr-hook-secret"
                      }
                    }
                  ]
                }
              ],
              "timeout": "10m"
            }
          }
        ]
      }
    ]
  }
}
```

### 3.5 DisasterInstance 传参

容灾实例使用同一套 Velero 原生 schema，仅改变字段位置：

```json
{
  "veleroHooks": {
    "dataBackup": {
      "resources": [
        {
          "name": "data-quiesce",
          "includedResources": ["pods"],
          "labelSelector": {
            "matchLabels": {
              "app": "demo"
            }
          },
          "pre": [
            {
              "exec": {
                "container": "app",
                "command": ["/usr/local/bin/dr-hook", "pre-backup", "--mode", "quiesce"],
                "onError": "Fail",
                "timeout": "30s"
              }
            }
          ],
          "post": [
            {
              "exec": {
                "container": "app",
                "command": ["/usr/local/bin/dr-hook", "post-backup", "--mode", "resume"],
                "onError": "Continue",
                "timeout": "30s"
              }
            }
          ]
        }
      ]
    },
    "dataRestore": {
      "resources": [
        {
          "name": "data-after-restore",
          "includedResources": ["pods"],
          "postHooks": [
            {
              "exec": {
                "container": "app",
                "command": ["/usr/local/bin/dr-hook", "after-restore", "--target", "standby"],
                "onError": "Fail",
                "waitForReady": true,
                "waitTimeout": "5m",
                "execTimeout": "1m"
              }
            }
          ]
        }
      ]
    }
  }
}
```

如果 `includedNamespaces` 为空，Velero hook 会在 Velero 任务的资源范围内匹配。对于 DisasterInstance，资源范围仍由实例 `spec.namespaces`、`labelSelector` 和 DataSync 构建出的 AppBackup/AppRestore 限定。

### 3.6 敏感参数规则

禁止把密码、token、access key 等敏感值直接写入 `command`、`args` 或 Hook JSON。原因是这些值会进入 Kubernetes CRD、审计日志、备份/恢复详情和可能的前端响应。

敏感参数应通过以下方式提供：

- 业务容器已有的 Secret env 或挂载文件。
- Restore initContainer 的 `envFrom.secretRef` 或 `env.valueFrom.secretKeyRef`。
- 应用侧 wrapper script 自行从受控路径读取。

server 校验阶段必须对明显的敏感字段名或命令片段硬拒绝，例如 `password=`、`token=`、`access_key=`、`secret=`。错误码使用 `VeleroHookSensitiveParameter`，错误元数据必须包含字段路径，例如 `hooks.resources[0].pre[0].exec.command[2]` 或 `veleroHooks.dataRestore.resources[0].postHooks[0].init.initContainers[0].args[0]`。UI 可以在提交前做风险提示，但 server 不提供“提示后仍允许保存”的绕过路径。

## 4. 合并与清空语义

### 4.1 create

create 请求中提供 `hooks` / `veleroHooks` 时完整写入 CRD。未提供时保持空值。

### 4.2 update

update 需要区分三种语义：

- 字段未出现：保持原值。
- 字段出现且为对象：替换原值。
- 字段出现且为空对象或 `null`：清空原值。

server DTO 需要使用 RawMessage 或显式 presence flag 识别字段是否出现，不能只依赖 Go 零值。对于 Hook 字段，替换语义是整体替换，不做按 `resources[].name` 的局部 merge。

CRD 层的 `DisasterVeleroHooks.dataBackup` 与 `dataRestore` 必须使用指针字段，以保留 nil、空对象、非空对象的语义。server DTO 还必须分别记录 `veleroHooks`、`veleroHooks.dataBackup`、`veleroHooks.dataRestore` 是否出现，保证子字段级 update/clear 可判定。

## 5. 基础校验

本阶段不做命令白名单，但必须做基础结构校验：

- Backup hook `resources[].includedResources` 为空或包含 `pods` 时才允许；显式只配置非 Pod 资源时拒绝。
- Restore hook 同理，必须能作用到 Pod。
- `exec.command` 必须非空。
- `exec.command` 必须保持数组顺序，server/operator 不得重排、拼接或拆分参数。
- `onError` 只能为 `Fail`、`Continue` 或空。
- timeout 字段必须为正数，并遵守平台最大值：Backup exec `timeout` 最大 10m，Restore exec `execTimeout` 最大 10m，Restore exec `waitTimeout` 最大 30m，Restore init `timeout` 最大 30m。
- hook 的 namespace 不应全部落在备份/恢复范围外；能静态判断无命中时应拒绝或给出明确错误。
- 不支持平台占位符；检测到 `${testudo.`、`{{` 等未来模板语法时，第一阶段必须拒绝。

## 6. 状态回显

Velero `BackupStatus` 和 `RestoreStatus` 已包含 `hookStatus`，server DTO 需要继续回显：

- `hooksAttempted`
- `hooksFailed`

AppBackup/AppRestore 详情和历史直接回显 Velero status 中的 `hookStatus`。容灾同步历史必须通过 `SyncHistoryRecord.backupHookStatus` / `restoreHookStatus` 回显，避免 Web 侧临时关联二级资源。详细命令日志第一阶段不纳入范围。

## 7. OpenAPI / RunAPI

server 实现时必须同步：

- AppBackup create/update/detail/list schema。
- AppRestore create/update/detail/list schema。
- DisasterInstance create/update/detail/list schema。
- DisasterDrill create/update/detail/list schema。
- 对应 RunAPI/Apipost 文档与示例。

## 8. 测试策略

Operator：

- AppBackup 创建 Velero Backup 时保留 template hooks。
- AppBackup 创建 Velero Schedule 时保留 template hooks。
- AppRestore 创建 Velero Restore 时保留 template hooks。
- DataSync build AppBackupSpec 时投影 instance dataBackup hooks。
- DataSync 既有 AppBackup desired template 对齐，覆盖 hooks 变更。
- DataSync build AppRestoreSpec 时投影 instance dataRestore hooks。
- DataSync 同步历史记录 backupHookStatus / restoreHookStatus。
- Drill data restore 使用 drill dataRestore 覆盖实例 dataRestore。
- DrillReconciler 将 drill.spec.veleroHooks 复制到 operation.spec.drillConfig.veleroHooks。
- ResourceSync 不投影 hooks，并有测试锁定该限制。
- Hook command 数组参数顺序在各投影链路中保持不变。

Server：

- create/update/clear hooks 字段的 DTO 转换测试。
- veleroHooks 子字段 presence/clear 测试。
- hooks 基础校验测试。
- 敏感参数硬拒绝测试，断言错误码和字段路径。
- timeout 最大值校验测试。
- 模板占位符不渲染/拒绝测试。
- DTO 响应回显测试。
- 同步历史 DTO 回显 backupHookStatus / restoreHookStatus 测试。

Web：

- 高级 JSON 输入的表单回显、编辑、清空。
- hookStatus 展示。

## 9. 后续模板阶段

HookTemplate 暂不进入本变更。后续如果需要模板，可在不破坏当前透传字段的前提下，将模板渲染结果写入同一组 `hooks` / `veleroHooks` 字段。
