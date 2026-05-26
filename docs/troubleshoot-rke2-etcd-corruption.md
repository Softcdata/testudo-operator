# 故障排查与修复记录：RKE2 节点因 Etcd (bbolt) 数据损坏导致无法启动

## 1. 问题背景 (Background)
在 `rke201 (192.168.120.220)` 节点经历突然断电重启后，RKE2 服务无法启动。用户反馈系统持续报错“连接 Etcd 被拒绝”。

## 2. 问题现象 (Symptoms)
- `systemctl status rke2-server` 显示服务处于 `activating` 或 `failed` 状态。
- `journalctl -u rke2-server` 中观察到 Go 运行时 Panic 报错：
  ```text
  panic: freepages: failed to get all reachable pages (the first key[0]=(hex)... on leaf page(15070) needs to be >= the key in the ancestor (...). Stack: [14089 15070])
  ```
- 虽然通过 `ss -tunlp` 可以看到 6443 端口在监听，但 `kubectl` 操作报错 `EOF` 或 `connection reset by peer`，这表明 API Server 虽然进程在，但由于后端数据存储（Etcd）不可用而处于非健康状态。

## 3. 根本原因分析 (Root Cause)
故障源于 **Etcd 底层数据库 (bbolt) 损坏**。
- **bbolt 机制**：Etcd 使用 bbolt 作为单文件 KV 存储。非正常断电会导致内存中的脏页未能及时刷入磁盘，触发 `freelist` 页面映射结构错乱。
- **Panic 触发**：当 RKE2 启动临时 etcd 尝试同步数据时，bbolt 检测到页面 B-Tree 结构不一致，为了保护数据一致性选择直接 Panic。

## 4. 故障排查步骤 (Troubleshooting)
1. **核实进程冲突**：发现手动执行 `rke2 server` 与 `systemctl` 启动的服务进程冲突，导致多个 `containerd` 实例竞争。
2. **验证数据库完整性**：使用 `bbolt check` 工具对 `/var/lib/rancher/rke2/server/db/etcd/member/snap/db` 进行检查，确认报错。
3. **寻找备份**：检查 `/var/lib/rancher/rke2/server/db/snapshots`，确认存在断电前的健康快照。

## 5. 修复方案 (Resolution)

### 步骤 1：彻底清理运行环境
在进行数据恢复前，必须确保没有任何残留进程（包括埋伏在后台的 containerd 和 shim）：
```bash
systemctl disable --now rke2-server
/usr/local/bin/rke2-killall.sh  # 该脚本会清理挂载点和残留进程
```

### 步骤 2：从健康快照恢复
选择断电前最近的一个快照（例如 `Feb 27 12:00` 的版本）进行恢复。这一步会重置集群元数据并将数据回滚到快照时间点：
```bash
rke2 server --cluster-reset \
  --cluster-reset-restore-path=/var/lib/rancher/rke2/server/db/snapshots/etcd-snapshot-rke201-1772164803
```

### 步骤 3：正常启动服务
恢复完成后，重新启用并启动服务：
```bash
systemctl enable --now rke2-server
```

## 6. 验证结果 (Verification)
- **节点状态**：`kubectl get nodes` 显示 `rke201` 恢复为 `Ready` 状态。
- **Etcd 健康度**：日志中输出 `etcd data store connection OK`，且 `bbolt check` 验证通过。
- **集群状态**：
  ```text
  NAME     STATUS   ROLES                       AGE    VERSION
  rke201   Ready    control-plane,etcd,master   382d   v1.30.9+rke2r1
  rke202   Ready    worker                      382d   v1.30.9+rke2r1
  ```

## 7. 经验总结 (Lessons Learned)
1. **多节点冗余**：本案例中 `rke202` 为 Worker，如果集群有 3 个或以上 Server 节点，单点 Etcd 损坏可能不会导致全集群不可用。
2. **备份重要性**：RKE2 默认开启的 Etcd 自动快照（默认 12 小时一次）是此次能够快速恢复的关键。
3. **彻底停旧再启新**：在修集群时，一定要使用 `killall` 脚本确保环境干净，否则残留的 `containerd` 会导致各种诡异的端口或 Socket 冲突。
