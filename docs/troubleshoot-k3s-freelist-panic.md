# 故障排查与修复记录：K3s 节点因 Containerd bbolt 底层数据损坏导致无法启动

## 1. 问题现象 (Issue Description)
在 `ip170` 节点上，发现 `k3s` 服务处于不断重启的死循环中，无法正常运行。通过执行 `systemctl status k3s` 检查日志，观察到 Kubelet 等核心组件持续报出网络连接被拒绝的错误：
```text
transport: Error while dialing: dial unix /run/k3s/containerd/containerd.sock: connect: connection refused
```
这表明 K3s 集成的底层容器引擎 `containerd` 并没有正常运行，导致 K3s 的控制平面无法与其通过 UNIX Socket 进行通信。

## 2. 排查思路 (Troubleshooting Process)
针对上述现象，排查的整体思路如下：

1. **确定异常主体**：K3s 报错明确指向 `containerd.sock` 连接被拒绝，说明真正崩溃的主体是底层的 `containerd` 进程，而非 K3s 控制节点自身的问题。
2. **下沉排查底层服务**：通过梳理进程发现 K3s 内部是通过拉起 `containerd` 来管理容器生命周期的。因此我们直接转去查看 `containerd` 的运行状态和系统崩溃日志（`journalctl -u containerd`）。
3. **定位致命错误 (Fatal Error)**：在持续滚动的 `containerd` 日志中，我们寻找导致进程中断重启的那条决定性 `panic` 报错。
4. **追溯上下文环境**：通过查看触发 `panic` 的调用栈（Goroutine stack trace），定位到崩溃发生模块位于 `go.etcd.io/bbolt/internal/freelist` 以及 `github.com/containerd/containerd/v2/plugins/snapshots/overlay`。由此得出判断——这不是运行时资源耗尽的问题，而是磁盘持久化数据在文件底层发生的介质级损坏。

## 3. 根本原因 (Root Cause)
通过日志捕获到了导致 `containerd` 不断崩溃的致命错误：
```text
panic: invalid freelist page: 62, page type is leaf
goroutine 1209 [running]:
go.etcd.io/bbolt/internal/freelist.(*shared).Read(...)
...
github.com/containerd/containerd/v2/core/snapshots/storage.(*MetaStore).TransactionContext(...)
...
github.com/containerd/containerd/v2/plugins/snapshots/overlay.(*snapshotter).createSnapshot(...)
```

**原因定性：**
`containerd` 在本地磁盘使用了 `bbolt`（一个 Go 语言编写的单文件 KV 数据库）来持久化存储容器和镜像的元数据（主要分布在 `/var/lib/rancher/k3s/agent/containerd/.../meta.db` 中）。

报错信息 `panic: invalid freelist page, page type is leaf` 明确表示 **底层的 bbolt 数据库文件（meta.db）已遭到物理损坏，或者出现了内部结构错乱**。
引起此系统级异常的常见场景包括：
- 节点遭遇**非正常关机、异常断电或内核崩溃直接重启**，导致内存中的脏数据页（Dirty Pages）未能正确 Flush 刷入磁盘中。
- 底层存储卷发生了 IO 写入错误。

由于数据库文件损坏，当 `containerd` 尝试拉起并读取本地镜像快照树信息（OverlayFS）时，直接触发了 Go 语言底层的 Panic 保护机制而强制退出进程，继而导致上层的 `k3s.service` 也因为读不到 Socket 而陷入死循环自启。

## 4. 解决方案 (Solution)
对于 Kubernetes / K3s 这种高度强调“无状态自愈”和“声明式”系统而言，最迅速且极度安全的修复方式是：**直接清理掉 `containerd` 在本地缓存的旧元数据结构，迫使它重启时建立干净的初始数据缓冲区。** 
*(注：清空上述缓存目录不会损坏或丢失在 Etcd/Kine 存储的 Kubernetes 业务核心资源定义，仅仅会让节点丢失本地镜像与容器的快照层映射，重启后系统会自动从远程再拉取数据补全该状态)*。

**修复方案的完整执行步骤：**

### 步骤 1：停止互相自启的进程守护
在操作文件系统前，必须先彻底停掉 K3s 和 Containerd 面板，防止两边在删除期间因为后台心跳重建触发资源死锁。
```bash
systemctl stop k3s containerd
```

### 步骤 2：阻断并删除损坏的本地状态缓存
直接将 K3s 内部使用的 Containerd 代理存储目录从全局移除（Containerd 重新拉起时会检查自身工作路径如为空，则按纯净新节点重新初化结构）：
```bash
rm -rf /var/lib/rancher/k3s/agent/containerd
```
*(安全替代方案：如果仅想精准丢弃损坏的库，也可以通过 `find /var/lib/rancher/k3s/agent/containerd -name "*.db"` 找出所有损坏的文件进行备份隔离或删除。由于本文追求最快解法，采取全局抹除缓存法)*

### 步骤 3：重新拉起服务恢复调度
持久化污点清理干净后，先启动下层容器引擎，再唤醒 K3s 集群底座：
```bash
systemctl start containerd k3s
```

## 5. 验证与总结 (Verification & Conclusion)
服务拉起以后，执行如下命令核验进程健康状态：
```bash
systemctl status k3s
systemctl status containerd
```
检查节点容器是否开始在正常调度生成：
```bash
kubectl get pods -A
```
**总结记录：** 
执行清库抹除操作后，`containerd` 已顺畅建立起全新的健康的 `bbolt` 树结构文件，不再宕机。目前 `ip170` 节点上的 K3s 引擎完全恢复了高可用性服务，系统网络 Socket 堵死问题圆满解除。之前断掉的数据包和未拉起的业务 Pod 也已经自动重新分配调度！
