package apprestore

import (
	"context"
	"strings"

	controller "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// getVeleroRestore tries to find the Velero Restore using the new naming convention ("res-")
// and then falls back to the old naming convention ("app-restore-") for backward compatibility.
func (r *AppRestoreReconciler) getVeleroRestore(ctx context.Context, cli client.Client, appRestore *disasterv1.AppRestore) (*velerov1.Restore, error) {
	logger := logf.FromContext(ctx)
	// 1. Try new name: res-{Name}
	newName := "res-" + appRestore.Name
	restore := &velerov1.Restore{}
	err := cli.Get(ctx, types.NamespacedName{Name: newName, Namespace: controller.VeleroNamespace}, restore)
	if err == nil {
		// Best-effort backfill: historical restores may not have cleanup labels.
		if labels, changed := EnsureCleanupLabels(restore.Labels, CleanupDescriptor{
			OwnerUID:     string(appRestore.UID),
			RelationCode: "finalizer.veleroRestore",
			Strategy:     CleanupStrategyDelete,
		}); changed {
			restore.Labels = labels
			_ = cli.Update(ctx, restore)
		}
		return restore, nil
	}
	if !apierrors.IsNotFound(err) && !apimeta.IsNoMatchError(err) {
		return nil, err
	}

	// 2. List by AppRestore UID label and pick latest object to tolerate naming changes/recreate windows.
	uid := strings.TrimSpace(string(appRestore.UID))
	if uid != "" {
		restoreList := &velerov1.RestoreList{}
		if errList := cli.List(ctx, restoreList,
			client.InNamespace(controller.VeleroNamespace),
			client.MatchingLabels{LabelAppRestoreUID: uid},
		); errList != nil {
			if !apimeta.IsNoMatchError(errList) {
				return nil, errList
			}
			logger.Info("Velero Restore kind not registered while listing by UID, skipping UID fallback", "appRestore", appRestore.Name)
		}
		if len(restoreList.Items) > 0 {
			latestIdx := 0
			for i := 1; i < len(restoreList.Items); i++ {
				cur := restoreList.Items[i]
				best := restoreList.Items[latestIdx]
				if cur.CreationTimestamp.After(best.CreationTimestamp.Time) ||
					(cur.CreationTimestamp.Equal(&best.CreationTimestamp) && cur.Name > best.Name) {
					latestIdx = i
				}
			}
			if len(restoreList.Items) > 1 {
				logger.Info("multiple Velero Restore objects match AppRestore UID, selecting latest",
					"appRestore", appRestore.Name,
					"uid", uid,
					"count", len(restoreList.Items),
					"selected", restoreList.Items[latestIdx].Name,
				)
			}

			restoreByUID := restoreList.Items[latestIdx].DeepCopy()
			if labels, changed := EnsureCleanupLabels(restoreByUID.Labels, CleanupDescriptor{
				OwnerUID:     string(appRestore.UID),
				RelationCode: "finalizer.veleroRestore",
				Strategy:     CleanupStrategyDelete,
			}); changed {
				restoreByUID.Labels = labels
				_ = cli.Update(ctx, restoreByUID)
			}
			return restoreByUID, nil
		}
	}

	// 3. Try old name: app-restore-{Name}
	oldName := "app-restore-" + appRestore.Name
	restoreOld := &velerov1.Restore{}
	errOld := cli.Get(ctx, types.NamespacedName{Name: oldName, Namespace: controller.VeleroNamespace}, restoreOld)
	if errOld == nil {
		// Best-effort backfill for legacy-named resources.
		if labels, changed := EnsureCleanupLabels(restoreOld.Labels, CleanupDescriptor{
			OwnerUID:     string(appRestore.UID),
			RelationCode: "finalizer.veleroRestore",
			Strategy:     CleanupStrategyDelete,
		}); changed {
			restoreOld.Labels = labels
			_ = cli.Update(ctx, restoreOld)
		}
		return restoreOld, nil
	}
	if !apierrors.IsNotFound(errOld) && !apimeta.IsNoMatchError(errOld) {
		return nil, errOld
	}

	// Both not found, return NotFound error for the new name
	return nil, apierrors.NewNotFound(velerov1.Resource("restore"), newName)
}
