package disasterinstance

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/metadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newDisasterInstanceTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := disasterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}
	return scheme
}

func TestEnsureDataSyncWritesCleanupLabels(t *testing.T) {
	ctx := context.Background()
	scheme := newDisasterInstanceTestScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &DisasterInstanceReconciler{Client: cli, Scheme: scheme}

	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "instance-a",
			Namespace: "default",
			UID:       types.UID("instance-cleanup-uid"),
			Annotations: map[string]string{
				AnnotationTraceID: "trace-instance-a",
			},
		},
	}
	config := &disasterv1.DisasterConfig{}

	if err := reconciler.ensureDataSync(ctx, logr.Discard(), instance, config, "di-ds-a"); err != nil {
		t.Fatalf("ensureDataSync returned error: %v", err)
	}

	dataSync := &disasterv1.DataSync{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: "di-ds-a", Namespace: "default"}, dataSync); err != nil {
		t.Fatalf("get datasync: %v", err)
	}

	ownerToken := metadata.BuildDependencyToken(string(instance.UID))
	if got := dataSync.Labels[metadata.LabelCleanupOwnerToken]; got != ownerToken {
		t.Fatalf("unexpected cleanup owner token: got %q want %q", got, ownerToken)
	}
	if got := dataSync.Labels[metadata.LabelCleanupRelation]; got != "ownerReference.dataSync" {
		t.Fatalf("unexpected cleanup relation: %q", got)
	}
	if got := dataSync.Labels[metadata.LabelCleanupStrategy]; got != metadata.CleanupStrategyOwnerReference {
		t.Fatalf("unexpected cleanup strategy: %q", got)
	}
	if len(dataSync.OwnerReferences) != 1 || dataSync.OwnerReferences[0].UID != instance.UID {
		t.Fatalf("expected datasync owner reference to point to disasterinstance")
	}
}

func TestEnsureResourceSyncWritesCleanupLabels(t *testing.T) {
	ctx := context.Background()
	scheme := newDisasterInstanceTestScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &DisasterInstanceReconciler{Client: cli, Scheme: scheme}

	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "instance-b",
			Namespace: "default",
			UID:       types.UID("instance-resource-cleanup-uid"),
		},
	}
	config := &disasterv1.DisasterConfig{}

	if err := reconciler.ensureResourceSync(ctx, logr.Discard(), instance, config, "di-rs-b"); err != nil {
		t.Fatalf("ensureResourceSync returned error: %v", err)
	}

	resourceSync := &disasterv1.ResourceSync{}
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Name: "di-rs-b", Namespace: "default"}, resourceSync); err != nil {
		t.Fatalf("get resourcesync: %v", err)
	}

	ownerToken := metadata.BuildDependencyToken(string(instance.UID))
	if got := resourceSync.Labels[metadata.LabelCleanupOwnerToken]; got != ownerToken {
		t.Fatalf("unexpected cleanup owner token: got %q want %q", got, ownerToken)
	}
	if got := resourceSync.Labels[metadata.LabelCleanupRelation]; got != "ownerReference.resourceSync" {
		t.Fatalf("unexpected cleanup relation: %q", got)
	}
	if got := resourceSync.Labels[metadata.LabelCleanupStrategy]; got != metadata.CleanupStrategyOwnerReference {
		t.Fatalf("unexpected cleanup strategy: %q", got)
	}
	if len(resourceSync.OwnerReferences) != 1 || resourceSync.OwnerReferences[0].UID != instance.UID {
		t.Fatalf("expected resourcesync owner reference to point to disasterinstance")
	}
	if resourceSync.Spec.StandbyModifier == nil {
		t.Fatalf("expected standby modifier to be initialized")
	}
}
