package resourcesync

import (
	"context"
	"crypto/md5"
	"fmt"

	ctrlpkg "github.com/softcdata/testudo-operator/internal/controller"
	restorebuilder "github.com/softcdata/testudo-operator/internal/controller/restore"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type resourceSyncRestorePhaseKind string

const (
	resourceSyncRestorePhaseLegacy    resourceSyncRestorePhaseKind = "legacy"
	resourceSyncRestorePhaseCluster   resourceSyncRestorePhaseKind = "cluster"
	resourceSyncRestorePhaseNamespace resourceSyncRestorePhaseKind = "namespace"
)

func planResourceSyncRestorePhases(instance *disasterv1.DisasterInstance) []resourceSyncRestorePhaseKind {
	selectionPlan := resolveResourceSyncSelectionPlan(instance)
	if !selectionPlan.scopedMode() {
		return []resourceSyncRestorePhaseKind{resourceSyncRestorePhaseLegacy}
	}

	phases := make([]resourceSyncRestorePhaseKind, 0, 2)
	if selectionPlan.clusterPhaseEnabled() {
		phases = append(phases, resourceSyncRestorePhaseCluster)
	}
	if selectionPlan.namespacePhaseEnabled() {
		phases = append(phases, resourceSyncRestorePhaseNamespace)
	}
	return phases
}

func resourceSyncRestoreName(resourceSyncName string, backupName string, phase resourceSyncRestorePhaseKind) string {
	rsName := resourceSyncName
	if len(rsName) > 20 {
		rsName = rsName[:20]
	}
	backupHash := fmt.Sprintf("%x", md5.Sum([]byte(backupName)))[:6]

	switch phase {
	case resourceSyncRestorePhaseCluster:
		return fmt.Sprintf("rec-rs-%s-%s-cluster", rsName, backupHash)
	case resourceSyncRestorePhaseNamespace:
		return fmt.Sprintf("rec-rs-%s-%s-ns", rsName, backupHash)
	default:
		return fmt.Sprintf("rec-rs-%s-%s", rsName, backupHash)
	}
}

func (r *ResourceSyncReconciler) buildAppRestoreSpec(
	ctx context.Context,
	rs *disasterv1.ResourceSync,
	config *disasterv1.DisasterConfig,
	instance *disasterv1.DisasterInstance,
	backupName string,
) (disasterv1.AppRestoreSpec, restorebuilder.PolicySummary, error) {
	phase := resourceSyncRestorePhaseLegacy
	if resolveResourceSyncSelectionPlan(instance).scopedMode() {
		phase = resourceSyncRestorePhaseNamespace
	}
	return r.buildResourceSyncRestoreSpecForPhase(ctx, rs, config, instance, backupName, phase)
}

func (r *ResourceSyncReconciler) buildClusterAppRestoreSpec(
	ctx context.Context,
	rs *disasterv1.ResourceSync,
	config *disasterv1.DisasterConfig,
	instance *disasterv1.DisasterInstance,
	backupName string,
) (disasterv1.AppRestoreSpec, restorebuilder.PolicySummary, error) {
	return r.buildResourceSyncRestoreSpecForPhase(ctx, rs, config, instance, backupName, resourceSyncRestorePhaseCluster)
}

func (r *ResourceSyncReconciler) buildResourceSyncRestoreSpecForPhase(
	ctx context.Context,
	rs *disasterv1.ResourceSync,
	config *disasterv1.DisasterConfig,
	instance *disasterv1.DisasterInstance,
	backupName string,
	phase resourceSyncRestorePhaseKind,
) (disasterv1.AppRestoreSpec, restorebuilder.PolicySummary, error) {
	appBackupName := fmt.Sprintf("rs-%s", rs.Name)
	source, target := resolveClusters(instance, config)

	imageRules := []disasterv1.ResourceModifierRule(nil)
	if phase != resourceSyncRestorePhaseCluster {
		rules, err := r.buildImageRewriteRules(ctx, config, instance, source, target)
		if err != nil {
			return disasterv1.AppRestoreSpec{}, restorebuilder.PolicySummary{}, err
		}
		imageRules = rules
	}

	spec := restorebuilder.BuildAppRestoreSpec(restorebuilder.BuilderConfig{
		RestoreType:                restorebuilder.RestoreTypeResource,
		BackupSource:               appBackupName,
		BackupName:                 backupName,
		TargetCluster:              target,
		SourceCluster:              source,
		StorageRepository:          config.Spec.StorageRepository,
		IncludedNamespaces:         instance.Spec.Namespaces,
		IsForDrill:                 false,
		ExtraResourceModifierRules: imageRules,
	})

	var targetClient client.Client
	if restorebuilder.RequiresTargetClassValidation(instance) {
		c, err := ctrlpkg.GetKubeClientSet(ctx, r.Client, r.Scheme, target)
		if err != nil {
			return disasterv1.AppRestoreSpec{}, restorebuilder.PolicySummary{}, fmt.Errorf("build target cluster client for restore policy: %w", err)
		}
		targetClient = c
	}

	summary, err := restorebuilder.ApplyInstanceRestorePolicy(
		ctx,
		&spec,
		instance,
		targetClient,
		restorebuilder.WithBaselineClusters(config.Spec.SourceCluster, config.Spec.TargetCluster),
		restorebuilder.WithApplyTarget(disasterv1.RestoreModifierApplyResourceSync),
	)
	if err != nil {
		return disasterv1.AppRestoreSpec{}, restorebuilder.PolicySummary{}, err
	}

	selectionPlan := resolveResourceSyncSelectionPlan(instance)
	switch phase {
	case resourceSyncRestorePhaseCluster:
		applyResourceSyncClusterRestoreSelection(&spec, selectionPlan)
	case resourceSyncRestorePhaseNamespace:
		applyResourceSyncNamespacedRestoreSelection(&spec, selectionPlan)
	default:
		enforceResourceSyncMandatoryRestoreExclusions(&spec.Template)
	}

	return spec, summary, nil
}

func updateResourceSyncRestorePhaseStatus(
	resourceSync *disasterv1.ResourceSync,
	phase resourceSyncRestorePhaseKind,
	restoreName string,
	status disasterv1.AppRestorePhase,
) bool {
	if resourceSync == nil {
		return false
	}

	changed := false

	switch phase {
	case resourceSyncRestorePhaseCluster:
		if resourceSync.Status.LastNamespaceRestoreName == "" && resourceSync.Status.LastRestoreName != restoreName {
			resourceSync.Status.LastRestoreName = restoreName
			changed = true
		}
		if resourceSync.Status.LastClusterRestoreName != restoreName {
			resourceSync.Status.LastClusterRestoreName = restoreName
			changed = true
		}
		if resourceSync.Status.ClusterRestoreStatus != status {
			resourceSync.Status.ClusterRestoreStatus = status
			changed = true
		}
	case resourceSyncRestorePhaseNamespace:
		if resourceSync.Status.LastRestoreName != restoreName {
			resourceSync.Status.LastRestoreName = restoreName
			changed = true
		}
		if resourceSync.Status.LastNamespaceRestoreName != restoreName {
			resourceSync.Status.LastNamespaceRestoreName = restoreName
			changed = true
		}
		if resourceSync.Status.NamespaceRestoreStatus != status {
			resourceSync.Status.NamespaceRestoreStatus = status
			changed = true
		}
	default:
		if resourceSync.Status.LastRestoreName != restoreName {
			resourceSync.Status.LastRestoreName = restoreName
			changed = true
		}
		if resourceSync.Status.NamespaceRestoreStatus != status {
			resourceSync.Status.NamespaceRestoreStatus = status
			changed = true
		}
	}

	return changed
}

func lookupResourceSyncBackupCycle(appBackup *disasterv1.AppBackup, backupName string) (int, *metav1.Time) {
	if appBackup == nil {
		return 0, nil
	}
	for _, rec := range appBackup.Status.History {
		if rec.Name != backupName {
			continue
		}
		items := 0
		if rec.VeleroStatus != nil && rec.VeleroStatus.Progress != nil {
			items = rec.VeleroStatus.Progress.ItemsBackedUp
		}
		return items, rec.StartTimestamp
	}
	return 0, nil
}

func lookupResourceSyncRestoreItems(restore *disasterv1.AppRestore) int {
	if restore == nil || restore.Status.RestoreStatus.Progress == nil {
		return 0
	}
	return restore.Status.RestoreStatus.Progress.ItemsRestored
}
