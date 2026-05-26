---
created: 2026-01-15
status: completed
---

# 设计：修复副本数记录时机

## 问题分析

### 当前 Failover 流程
```
PauseSchedules → ScaleDownSource → FinalSync (ResourceSync) → ScaleUpTarget → SwitchRoles
                     ↓                     ↓
                 副本数=0            记录副本数=0 (错误!)
```

### 修复后 Failover 流程
```
PauseSchedules → ScaleDownSource → FinalSync (ResourceSync) → ScaleUpTarget → SwitchRoles
                     ↓                     
                 1. 先记录副本数
                 2. 再缩容到0
```

## 实现方案

### 修改 `executeScaleDownSource` 函数

在缩容之前，先扫描并记录所有 Deployment/StatefulSet 的当前副本数到 ConfigMap。

```go
func (r *DisasterOperationReconciler) executeScaleDownSource(ctx context.Context, instance *disasterv1.DisasterInstance) (bool, error) {
    // ... 获取 config 和 client ...

    // ===== 新增: 在缩容之前先记录副本数 =====
    if err := r.recordReplicasBeforeScaleDown(ctx, instance, targetClusterName); err != nil {
        // 记录失败不应阻塞 Failover，但要记录警告
        r.Log.Error(err, "Warning: failed to record replicas before scale down")
        r.Recorder.Eventf(instance, corev1.EventTypeWarning, "RecordReplicasFailed", 
            "Failed to record replicas: %v", err)
    }

    // 原有的缩容逻辑...
    for _, ns := range instance.Spec.Namespaces {
        // Deployments
        deployList := &appsv1.DeploymentList{}
        // ...
    }
}

// recordReplicasBeforeScaleDown 在缩容前记录副本数
func (r *DisasterOperationReconciler) recordReplicasBeforeScaleDown(
    ctx context.Context, 
    instance *disasterv1.DisasterInstance, 
    clusterName string,
) error {
    remoteClient, err := r.getClusterClient(ctx, clusterName)
    if err != nil {
        return err
    }

    replicasMap := make(map[string]int32)

    for _, ns := range instance.Spec.Namespaces {
        // Deployments
        deployList := &appsv1.DeploymentList{}
        if err := remoteClient.List(ctx, deployList, client.InNamespace(ns)); err != nil {
            return err
        }
        for _, deploy := range deployList.Items {
            if deploy.Spec.Replicas != nil && *deploy.Spec.Replicas > 0 {
                key := fmt.Sprintf("%s/deployments/%s", ns, deploy.Name)
                replicasMap[key] = *deploy.Spec.Replicas
            }
        }

        // StatefulSets
        stsList := &appsv1.StatefulSetList{}
        if err := remoteClient.List(ctx, stsList, client.InNamespace(ns)); err != nil {
            return err
        }
        for _, sts := range stsList.Items {
            if sts.Spec.Replicas != nil && *sts.Spec.Replicas > 0 {
                key := fmt.Sprintf("%s/statefulsets/%s", ns, sts.Name)
                replicasMap[key] = *sts.Spec.Replicas
            }
        }
    }

    // 序列化并保存到 ConfigMap
    data, err := json.Marshal(replicasMap)
    if err != nil {
        return err
    }

    cmName := fmt.Sprintf("replicas-%s", instance.Status.ResourceSyncName)
    cm := &corev1.ConfigMap{}
    if err := r.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: cmName}, cm); err != nil {
        if errors.IsNotFound(err) {
            // 创建新的 ConfigMap
            cm = &corev1.ConfigMap{
                ObjectMeta: metav1.ObjectMeta{
                    Name:      cmName,
                    Namespace: instance.Namespace,
                },
                Data: map[string]string{
                    "replicas": string(data),
                },
            }
            return r.Create(ctx, cm)
        }
        return err
    }

    // 更新现有的 ConfigMap
    cm.Data["replicas"] = string(data)
    return r.Update(ctx, cm)
}
```

## ConfigMap 命名约定

ConfigMap 名称: `replicas-{ResourceSyncName}`

示例: `replicas-dr-rs-auto-trigger-instance`

## 兼容性考虑

1. **ResourceSync 中的 `recordReplicasToConfigMap`**: 
   - 保留此函数，但改为**仅在副本数非零时更新**
   - 这样可以保留正常同步时的副本数记录功能
   - 如果副本数为0，说明是 Failover 场景，使用已有记录，不覆盖

2. **回退兼容**:
   - 如果 ConfigMap 不存在，`executeScaleUpTarget` 会使用 annotation 作为 fallback
   - annotation (`testudo.softcdata.com/original-replicas`) 目前在 scaleDown 时写入，保留此逻辑作为双保险
