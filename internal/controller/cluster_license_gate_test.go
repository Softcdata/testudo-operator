package controller

import (
	"context"
	"testing"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	platformlicense "github.com/softcdata/testudo-operator/pkg/license"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestClusterLicenseGateAcceptsFirstTwoAndRejectsThird(t *testing.T) {
	ctx := context.Background()
	scheme := newLicenseGateTestScheme(t)
	enabledAt := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	createdAt := metav1.NewTime(enabledAt.Add(time.Hour))
	client := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			licenseGateNamespace(),
			licenseGateState(enabledAt),
			licenseGateCluster("cluster-1", createdAt, nil),
			licenseGateCluster("cluster-2", createdAt, nil),
			licenseGateCluster("cluster-3", createdAt, nil),
		).
		WithStatusSubresource(&disasterv1.Cluster{}).
		Build()
	reconciler := &ClusterReconciler{
		Client:             client,
		Scheme:             scheme,
		Recorder:           record.NewFakeRecorder(20),
		LicenseGateEnabled: true,
		LicenseNamespace:   platformlicense.DefaultLicenseNamespace,
	}

	for _, name := range []string{"cluster-1", "cluster-2"} {
		cluster := &disasterv1.Cluster{}
		if err := client.Get(ctx, clientObjectKey(name), cluster); err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		accepted, err := reconciler.ensureClusterLicenseAccepted(ctx, cluster)
		if err != nil {
			t.Fatalf("accept %s: %v", name, err)
		}
		if !accepted {
			t.Fatalf("expected %s to be accepted", name)
		}
		if cluster.Annotations[platformlicense.AnnotationLicenseAccepted] != "true" {
			t.Fatalf("expected %s accepted annotation", name)
		}
	}

	cluster := &disasterv1.Cluster{}
	if err := client.Get(ctx, clientObjectKey("cluster-3"), cluster); err != nil {
		t.Fatalf("get cluster-3: %v", err)
	}
	accepted, err := reconciler.ensureClusterLicenseAccepted(ctx, cluster)
	if err != nil {
		t.Fatalf("reject third cluster: %v", err)
	}
	if accepted {
		t.Fatalf("expected third cluster to be rejected")
	}
	if cluster.Status.Reason != platformlicense.ReasonLicenseLimitExceeded {
		t.Fatalf("expected limit exceeded reason, got %q", cluster.Status.Reason)
	}
}

func TestClusterLicenseGateIgnoresTamperedStatusConfigMap(t *testing.T) {
	ctx := context.Background()
	scheme := newLicenseGateTestScheme(t)
	enabledAt := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	createdAt := metav1.NewTime(enabledAt.Add(time.Hour))
	client := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			licenseGateNamespace(),
			licenseGateState(enabledAt),
			tamperedActiveStatusConfigMap(),
			licenseGateCluster("cluster-1", createdAt, map[string]string{platformlicense.AnnotationLicenseAccepted: "true"}),
			licenseGateCluster("cluster-2", createdAt, map[string]string{platformlicense.AnnotationLicenseAccepted: "true"}),
			licenseGateCluster("cluster-3", createdAt, nil),
		).
		WithStatusSubresource(&disasterv1.Cluster{}).
		Build()
	reconciler := &ClusterReconciler{
		Client:             client,
		Scheme:             scheme,
		Recorder:           record.NewFakeRecorder(20),
		LicenseGateEnabled: true,
		LicenseNamespace:   platformlicense.DefaultLicenseNamespace,
	}

	cluster := &disasterv1.Cluster{}
	if err := client.Get(ctx, clientObjectKey("cluster-3"), cluster); err != nil {
		t.Fatalf("get cluster-3: %v", err)
	}
	accepted, err := reconciler.ensureClusterLicenseAccepted(ctx, cluster)
	if err != nil {
		t.Fatalf("gate third cluster: %v", err)
	}
	if accepted {
		t.Fatalf("tampered status ConfigMap must not grant unlimited clusters")
	}
	if cluster.Status.Reason != platformlicense.ReasonLicenseLimitExceeded {
		t.Fatalf("expected limit exceeded reason, got %q", cluster.Status.Reason)
	}
}

func TestClusterLicenseGateGrandfathersPreGateCluster(t *testing.T) {
	ctx := context.Background()
	scheme := newLicenseGateTestScheme(t)
	enabledAt := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	oldCreatedAt := metav1.NewTime(enabledAt.Add(-time.Hour))
	newCreatedAt := metav1.NewTime(enabledAt.Add(time.Hour))
	client := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			licenseGateNamespace(),
			licenseGateState(enabledAt),
			licenseGateCluster("cluster-1", newCreatedAt, map[string]string{platformlicense.AnnotationLicenseAccepted: "true"}),
			licenseGateCluster("cluster-2", newCreatedAt, map[string]string{platformlicense.AnnotationLicenseAccepted: "true"}),
			licenseGateCluster("legacy-cluster", oldCreatedAt, nil),
		).
		WithStatusSubresource(&disasterv1.Cluster{}).
		Build()
	reconciler := &ClusterReconciler{
		Client:             client,
		Scheme:             scheme,
		Recorder:           record.NewFakeRecorder(20),
		LicenseGateEnabled: true,
		LicenseNamespace:   platformlicense.DefaultLicenseNamespace,
	}

	cluster := &disasterv1.Cluster{}
	if err := client.Get(ctx, clientObjectKey("legacy-cluster"), cluster); err != nil {
		t.Fatalf("get legacy-cluster: %v", err)
	}
	accepted, err := reconciler.ensureClusterLicenseAccepted(ctx, cluster)
	if err != nil {
		t.Fatalf("grandfather legacy cluster: %v", err)
	}
	if !accepted {
		t.Fatalf("expected legacy cluster to be grandfathered")
	}
	if cluster.Annotations[platformlicense.AnnotationLicenseID] != platformlicense.LicenseIDGrandfathered {
		t.Fatalf("expected grandfathered license id, got %q", cluster.Annotations[platformlicense.AnnotationLicenseID])
	}
	if cluster.Annotations[platformlicense.AnnotationLicenseAcceptedReason] != platformlicense.LicenseAcceptedReasonPreGateUpgrade {
		t.Fatalf("expected grandfather reason, got %q", cluster.Annotations[platformlicense.AnnotationLicenseAcceptedReason])
	}
}

func newLicenseGateTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}
	return scheme
}

func licenseGateNamespace() *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: platformlicense.DefaultLicenseNamespace}}
}

func licenseGateState(enabledAt time.Time) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      platformlicense.GateStateConfigMapName,
			Namespace: platformlicense.DefaultLicenseNamespace,
		},
		Data: map[string]string{
			platformlicense.GateStateEnabledAtKey: enabledAt.UTC().Format(time.RFC3339),
		},
	}
}

func tamperedActiveStatusConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      platformlicense.StatusConfigMapName,
			Namespace: platformlicense.DefaultLicenseNamespace,
		},
		Data: map[string]string{
			"state":       "Active",
			"maxClusters": "-1",
		},
	}
}

func licenseGateCluster(name string, createdAt metav1.Time, annotations map[string]string) *disasterv1.Cluster {
	return &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: createdAt,
			Annotations:       annotations,
		},
		Spec: disasterv1.ClusterSpec{},
	}
}

func clientObjectKey(name string) client.ObjectKey {
	return client.ObjectKey{Name: name}
}
