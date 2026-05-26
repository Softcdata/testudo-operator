package resourcesync

import (
	"context"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *ResourceSyncReconciler) updateResourceSyncStatusWithRetry(
	ctx context.Context,
	resourceSync *disasterv1.ResourceSync,
	mutate func(*disasterv1.ResourceSync) bool,
) error {
	if resourceSync == nil {
		return nil
	}

	var updatedStatus disasterv1.ResourceSyncStatus
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &disasterv1.ResourceSync{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(resourceSync), latest); err != nil {
			return err
		}
		if !mutate(latest) {
			updatedStatus = latest.Status
			return nil
		}
		if err := r.Status().Update(ctx, latest); err != nil {
			return err
		}
		updatedStatus = latest.Status
		return nil
	})
	if err != nil {
		return err
	}

	resourceSync.Status = updatedStatus
	return nil
}
