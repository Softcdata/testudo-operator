package disasterdrill

import (
	"context"
	"fmt"
	"strings"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

const (
	drillNoDataVolumesCondition = "NoDataVolumes"
	drillNoPVCFoundReason       = "NoPVCFound"
	drillSkippedHistoryStatus   = "Skipped"
)

func classifyDrillRestoreMode(dataSync *disasterv1.DataSync, resourceSync *disasterv1.ResourceSync) (disasterv1.RestoreMode, error) {
	if resourceSync == nil {
		return "", fmt.Errorf("ResourceSync 未找到")
	}
	if resourceSync.Status.State != disasterv1.ResourceSyncStateReady {
		return "", fmt.Errorf("ResourceSync 状态不是 Ready，当前: %s", resourceSync.Status.State)
	}
	if strings.TrimSpace(resourceSync.Status.LastBackupName) == "" {
		return "", fmt.Errorf("ResourceSync 没有可用的资源备份")
	}

	if dataSync == nil {
		return "", fmt.Errorf("DataSync 未找到")
	}
	if dataSync.Status.State != disasterv1.DataSyncStateReady {
		return "", fmt.Errorf("DataSync 状态不是 Ready，当前: %s", dataSync.Status.State)
	}

	noDataCondition := apimeta.FindStatusCondition(dataSync.Status.Conditions, drillNoDataVolumesCondition)
	explicitNoData := noDataCondition != nil &&
		noDataCondition.Status == "True" &&
		noDataCondition.Reason == drillNoPVCFoundReason
	if explicitNoData {
		if len(dataSync.Status.History) == 0 {
			return "", fmt.Errorf("DataSync 声明 NoPVCFound，但缺少最新 Skipped 同步历史")
		}
		latest := dataSync.Status.History[len(dataSync.Status.History)-1]
		if latest.Status != drillSkippedHistoryStatus {
			return "", fmt.Errorf("DataSync 声明 NoPVCFound，但最新同步历史状态为 %s，不是 Skipped", latest.Status)
		}
		return disasterv1.RestoreModeResourceOnly, nil
	}

	if strings.TrimSpace(dataSync.Status.LastBackupName) == "" {
		return "", fmt.Errorf("DataSync 没有可用的数据备份，且未明确标记 NoPVCFound")
	}
	return disasterv1.RestoreModeFullRestore, nil
}

func (r *DisasterDrillReconciler) resolveInstanceRestoreMode(
	ctx context.Context,
	instance *disasterv1.DisasterInstance,
) (disasterv1.RestoreMode, error) {
	if instance == nil {
		return "", fmt.Errorf("DisasterInstance 为空")
	}
	if strings.TrimSpace(instance.Status.DataSyncName) == "" {
		return "", fmt.Errorf("实例 %s 未记录 DataSync 名称", instance.Name)
	}
	if strings.TrimSpace(instance.Status.ResourceSyncName) == "" {
		return "", fmt.Errorf("实例 %s 未记录 ResourceSync 名称", instance.Name)
	}

	dataSync := &disasterv1.DataSync{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.DataSyncName}, dataSync); err != nil {
		return "", fmt.Errorf("获取 DataSync %s 失败: %w", instance.Status.DataSyncName, err)
	}
	if dataSync.Spec.Instance != instance.Name {
		return "", fmt.Errorf("DataSync %s 关联实例为 %s，不是 %s", dataSync.Name, dataSync.Spec.Instance, instance.Name)
	}

	resourceSync := &disasterv1.ResourceSync{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: instance.Status.ResourceSyncName}, resourceSync); err != nil {
		return "", fmt.Errorf("获取 ResourceSync %s 失败: %w", instance.Status.ResourceSyncName, err)
	}
	if resourceSync.Spec.Instance != instance.Name {
		return "", fmt.Errorf("ResourceSync %s 关联实例为 %s，不是 %s", resourceSync.Name, resourceSync.Spec.Instance, instance.Name)
	}

	mode, err := classifyDrillRestoreMode(dataSync, resourceSync)
	if err != nil {
		return "", fmt.Errorf("实例 %s 备份校验失败: %w", instance.Name, err)
	}
	return mode, nil
}

func aggregateDrillRestoreMode(modes map[string]disasterv1.RestoreMode) disasterv1.RestoreMode {
	var aggregate disasterv1.RestoreMode
	for _, mode := range modes {
		if aggregate == "" {
			aggregate = mode
			continue
		}
		if aggregate != mode {
			return disasterv1.RestoreModeMixed
		}
	}
	return aggregate
}

func cloneRestoreModes(in map[string]disasterv1.RestoreMode) map[string]disasterv1.RestoreMode {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]disasterv1.RestoreMode, len(in))
	for name, mode := range in {
		out[name] = mode
	}
	return out
}

func restoreModesEqual(left, right map[string]disasterv1.RestoreMode) bool {
	if len(left) != len(right) {
		return false
	}
	for name, mode := range left {
		if right[name] != mode {
			return false
		}
	}
	return true
}

func setDrillRestoreModeSnapshot(drill *disasterv1.DisasterDrill, modes map[string]disasterv1.RestoreMode) {
	drill.Status.InstanceRestoreModes = cloneRestoreModes(modes)
	drill.Status.RestoreMode = aggregateDrillRestoreMode(modes)
}

func (r *DisasterDrillReconciler) collectCurrentRestoreModes(
	ctx context.Context,
	drill *disasterv1.DisasterDrill,
) (map[string]disasterv1.RestoreMode, error) {
	if drill.Spec.InstanceName != "" {
		instance := &disasterv1.DisasterInstance{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: drill.Namespace, Name: drill.Spec.InstanceName}, instance); err != nil {
			return nil, fmt.Errorf("获取 DisasterInstance %s 失败: %w", drill.Spec.InstanceName, err)
		}
		mode, err := r.resolveInstanceRestoreMode(ctx, instance)
		if err != nil {
			return nil, err
		}
		return map[string]disasterv1.RestoreMode{instance.Name: mode}, nil
	}

	group := &disasterv1.DisasterGroup{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: drill.Namespace, Name: drill.Spec.GroupName}, group); err != nil {
		return nil, fmt.Errorf("获取 DisasterGroup %s 失败: %w", drill.Spec.GroupName, err)
	}
	modes := make(map[string]disasterv1.RestoreMode)
	for _, level := range group.Spec.Levels {
		for _, instanceName := range level {
			if _, exists := modes[instanceName]; exists {
				continue
			}
			instance := &disasterv1.DisasterInstance{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: drill.Namespace, Name: instanceName}, instance); err != nil {
				return nil, fmt.Errorf("获取容灾组实例 %s 失败: %w", instanceName, err)
			}
			mode, err := r.resolveInstanceRestoreMode(ctx, instance)
			if err != nil {
				return nil, err
			}
			modes[instanceName] = mode
		}
	}
	if len(modes) == 0 {
		return nil, fmt.Errorf("DisasterGroup %s 不包含可演练实例", group.Name)
	}
	return modes, nil
}

func (r *DisasterDrillReconciler) refreshRestoreModeSnapshot(
	ctx context.Context,
	drill *disasterv1.DisasterDrill,
) (changed bool, reason string, err error) {
	current, err := r.collectCurrentRestoreModes(ctx, drill)
	if err != nil {
		return false, drillReasonBackupUnavailable, err
	}
	if len(drill.Status.InstanceRestoreModes) == 0 {
		setDrillRestoreModeSnapshot(drill, current)
		return true, "", nil
	}
	if !restoreModesEqual(drill.Status.InstanceRestoreModes, current) {
		return false, drillReasonBackupStateChanged, fmt.Errorf(
			"演练 Ready 后备份恢复模式发生变化，原快照=%v，当前=%v，请重新创建或重置演练",
			drill.Status.InstanceRestoreModes,
			current,
		)
	}
	aggregate := aggregateDrillRestoreMode(current)
	if drill.Status.RestoreMode != aggregate {
		drill.Status.RestoreMode = aggregate
		return true, "", nil
	}
	return false, "", nil
}
