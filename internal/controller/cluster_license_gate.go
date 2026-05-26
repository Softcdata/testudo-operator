package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	platformlicense "github.com/softcdata/testudo-operator/pkg/license"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *ClusterReconciler) ensureClusterLicenseAccepted(ctx context.Context, cluster *disasterv1.Cluster) (bool, error) {
	if cluster == nil {
		return true, nil
	}
	if !r.LicenseGateEnabled {
		return true, nil
	}
	if isLicenseAccepted(cluster) {
		return true, nil
	}

	logger := logf.FromContext(ctx)
	r.LicenseAcceptanceLock.Lock()
	defer r.LicenseAcceptanceLock.Unlock()

	latest := &disasterv1.Cluster{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
		return false, err
	}
	if !latest.ObjectMeta.DeletionTimestamp.IsZero() {
		return true, nil
	}
	if isLicenseAccepted(latest) {
		cluster.ObjectMeta = latest.ObjectMeta
		cluster.ResourceVersion = latest.ResourceVersion
		return true, nil
	}

	store := r.licenseStore()
	enabledAt, _, gateStateErr := store.EnsureGateState(ctx)
	if gateStateErr != nil {
		logger.Error(gateStateErr, "unable to ensure license gate state")
	}
	if shouldGrandfatherCluster(latest, enabledAt) {
		if err := r.acceptCluster(ctx, cluster, platformlicense.LicenseIDGrandfathered, platformlicense.LicenseAcceptedReasonPreGateUpgrade, storeNow(store)); err != nil {
			return false, err
		}
		return true, nil
	}

	entitlement := store.Evaluate(ctx, r.licenseVerifier())
	clusters := &disasterv1.ClusterList{}
	if err := r.List(ctx, clusters); err != nil {
		return false, err
	}
	totalCount, acceptedSiblingCount := countLicenseGateClusters(clusters, latest.Name)
	if err := store.UpsertStatus(ctx, entitlement, totalCount); err != nil {
		logger.Error(err, "unable to update license status configmap")
	}

	if !entitlement.CanAcceptCluster(acceptedSiblingCount) {
		limit := entitlement.ClusterLimit()
		message := fmt.Sprintf(
			"cluster license limit exceeded: accepted clusters=%d, limit=%d, license state=%s",
			acceptedSiblingCount,
			limit,
			entitlement.State,
		)
		cluster.Status.Status = disasterv1.ClusterStatusNotReady
		cluster.Status.Reason = platformlicense.ReasonLicenseLimitExceeded
		cluster.Status.Message = message
		helper.ReportDiagnosticEvent(r.Recorder, cluster, corev1.EventTypeWarning, platformlicense.ReasonLicenseLimitExceeded, message)
		return false, nil
	}

	licenseID := strings.TrimSpace(entitlement.LicenseID)
	if licenseID == "" {
		licenseID = string(entitlement.State)
	}
	if err := r.acceptCluster(ctx, cluster, licenseID, "", storeNow(store)); err != nil {
		return false, err
	}
	return true, nil
}

func (r *ClusterReconciler) acceptCluster(ctx context.Context, cluster *disasterv1.Cluster, licenseID, reason string, acceptedAt time.Time) error {
	acceptedAt = acceptedAt.UTC().Truncate(time.Second)
	return r.updateClusterMetadataWithRetry(ctx, cluster, func(latest *disasterv1.Cluster) {
		if latest.Annotations == nil {
			latest.Annotations = map[string]string{}
		}
		latest.Annotations[platformlicense.AnnotationLicenseAccepted] = "true"
		latest.Annotations[platformlicense.AnnotationLicenseAcceptedAt] = acceptedAt.Format(time.RFC3339)
		latest.Annotations[platformlicense.AnnotationLicenseID] = strings.TrimSpace(licenseID)
		if strings.TrimSpace(reason) != "" {
			latest.Annotations[platformlicense.AnnotationLicenseAcceptedReason] = strings.TrimSpace(reason)
		} else {
			delete(latest.Annotations, platformlicense.AnnotationLicenseAcceptedReason)
		}
	})
}

func (r *ClusterReconciler) licenseStore() platformlicense.KubernetesStore {
	return platformlicense.KubernetesStore{
		Client:    r.Client,
		Namespace: r.effectiveLicenseNamespace(),
		CAPath:    r.LicenseCAPath,
	}
}

func (r *ClusterReconciler) effectiveLicenseNamespace() string {
	if strings.TrimSpace(r.LicenseNamespace) != "" {
		return strings.TrimSpace(r.LicenseNamespace)
	}
	if strings.TrimSpace(r.ManagementNamespace) != "" {
		return strings.TrimSpace(r.ManagementNamespace)
	}
	return platformlicense.DefaultLicenseNamespace
}

func (r *ClusterReconciler) licenseVerifier() *platformlicense.Verifier {
	if r.LicenseVerifier != nil {
		return r.LicenseVerifier
	}
	return platformlicense.NewDefaultVerifier()
}

func isLicenseAccepted(cluster *disasterv1.Cluster) bool {
	if cluster == nil || cluster.Annotations == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cluster.Annotations[platformlicense.AnnotationLicenseAccepted]), "true")
}

func shouldGrandfatherCluster(cluster *disasterv1.Cluster, enabledAt time.Time) bool {
	if cluster == nil || enabledAt.IsZero() || cluster.CreationTimestamp.IsZero() {
		return false
	}
	return cluster.CreationTimestamp.Time.Before(enabledAt)
}

func countLicenseGateClusters(clusters *disasterv1.ClusterList, currentName string) (totalCount int, acceptedSiblingCount int) {
	if clusters == nil {
		return 0, 0
	}
	for i := range clusters.Items {
		item := &clusters.Items[i]
		if !item.DeletionTimestamp.IsZero() {
			continue
		}
		totalCount++
		if item.Name == currentName {
			continue
		}
		if isLicenseAccepted(item) {
			acceptedSiblingCount++
		}
	}
	return totalCount, acceptedSiblingCount
}

func storeNow(store platformlicense.KubernetesStore) time.Time {
	if store.Now != nil {
		return store.Now().UTC()
	}
	return time.Now().UTC()
}

func ensureLicenseGateStateForManager(ctx context.Context, cli client.Client, namespace string) error {
	store := platformlicense.KubernetesStore{Client: cli, Namespace: namespace}
	_, _, err := store.EnsureGateState(ctx)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func nonDeletingClusterCount(ctx context.Context, cli client.Client) (int, error) {
	clusters := &disasterv1.ClusterList{}
	if err := cli.List(ctx, clusters); err != nil {
		return 0, err
	}
	count, _ := countLicenseGateClusters(clusters, "")
	return count, nil
}

func licenseStatusEntitlement(ctx context.Context, cli client.Client, namespace string, verifier *platformlicense.Verifier) (platformlicense.Entitlement, int, error) {
	store := platformlicense.KubernetesStore{Client: cli, Namespace: namespace}
	entitlement := store.Evaluate(ctx, verifier)
	count, err := nonDeletingClusterCount(ctx, cli)
	if err != nil {
		return entitlement, 0, err
	}
	if err := store.UpsertStatus(ctx, entitlement, count); err != nil {
		return entitlement, count, err
	}
	return entitlement, count, nil
}
