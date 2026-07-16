package datasync

import (
	"context"
	"testing"

	ctrlcommon "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const expectedTargetTrafficlessImage = "harbor.target.local/platform/busybox:1.36"

func TestBuildAppRestoreSpec_UsesTargetClusterTrafficlessRegistry(t *testing.T) {
	ctx := context.Background()
	s := newDataSyncTrafficlessScheme(t)
	reconciler := &DataSyncReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(
				trafficlessCluster("cluster-a", "harbor.source.local/platform", ""),
				trafficlessCluster("cluster-b", "harbor.target.local/platform", ""),
			).
			Build(),
		Scheme: s,
	}

	ds := &disasterv1.DataSync{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"}}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{Namespaces: []string{"app-ns"}},
		Status: disasterv1.DisasterInstanceStatus{
			PrimaryCluster:   "cluster-a",
			SecondaryCluster: "cluster-b",
		},
	}
	config := trafficlessConfig("cluster-b")

	spec, _, err := reconciler.buildAppRestoreSpec(ctx, ds, config, instance, "backup-001")
	if err != nil {
		t.Fatalf("buildAppRestoreSpec returned error: %v", err)
	}
	if got := trafficlessPatchValue(spec.ResourceModifierRules, "/spec/containers/0/image"); got != expectedTargetTrafficlessImage {
		t.Fatalf("expected target cluster trafficless image, got %q", got)
	}
}

func TestBuildAppRestoreSpec_UsesReversedSecondaryClusterTrafficlessRegistry(t *testing.T) {
	ctx := context.Background()
	s := newDataSyncTrafficlessScheme(t)
	reconciler := &DataSyncReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(
				trafficlessCluster("cluster-a", "harbor.reverse.local/platform", ""),
				trafficlessCluster("cluster-b", "harbor.old-target.local/platform", ""),
			).
			Build(),
		Scheme: s,
	}

	ds := &disasterv1.DataSync{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"}}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{Namespaces: []string{"app-ns"}},
		Status: disasterv1.DisasterInstanceStatus{
			PrimaryCluster:   "cluster-b",
			SecondaryCluster: "cluster-a",
		},
	}
	config := trafficlessConfig("cluster-b")

	spec, _, err := reconciler.buildAppRestoreSpec(ctx, ds, config, instance, "backup-001")
	if err != nil {
		t.Fatalf("buildAppRestoreSpec returned error: %v", err)
	}
	if got := trafficlessPatchValue(spec.ResourceModifierRules, "/spec/containers/0/image"); got != "harbor.reverse.local/platform/busybox:1.36" {
		t.Fatalf("expected reversed secondary cluster trafficless image, got %q", got)
	}
}

func TestBuildAppRestoreSpec_TreatsHistoricalBusyboxDefaultsAsImplicit(t *testing.T) {
	ctx := context.Background()
	s := newDataSyncTrafficlessScheme(t)
	reconciler := &DataSyncReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(trafficlessCluster("cluster-b", "harbor.target.local/platform", "")).
			Build(),
		Scheme: s,
	}

	for _, image := range []string{"busybox:latest", "busybox:1.36"} {
		ds := &disasterv1.DataSync{
			ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"},
			Spec: disasterv1.DataSyncSpec{
				TrafficlessConfig: &disasterv1.TrafficlessConfig{Image: image},
			},
		}
		instance := &disasterv1.DisasterInstance{
			Spec:   disasterv1.DisasterInstanceSpec{Namespaces: []string{"app-ns"}},
			Status: disasterv1.DisasterInstanceStatus{PrimaryCluster: "cluster-a", SecondaryCluster: "cluster-b"},
		}

		spec, _, err := reconciler.buildAppRestoreSpec(ctx, ds, trafficlessConfig("cluster-b"), instance, "backup-001")
		if err != nil {
			t.Fatalf("buildAppRestoreSpec(%s) returned error: %v", image, err)
		}
		if got := trafficlessPatchValue(spec.ResourceModifierRules, "/spec/containers/0/image"); got != expectedTargetTrafficlessImage {
			t.Fatalf("expected historical default %s to resolve through target registry, got %q", image, got)
		}
	}
}

func TestBuildAppRestoreSpec_SyncsTrafficlessRegistrySecretToRestoreNamespace(t *testing.T) {
	ctx := context.Background()
	s := newDataSyncTrafficlessScheme(t)
	targetClient := fake.NewClientBuilder().WithScheme(s).Build()
	reconciler := &DataSyncReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(
				trafficlessCluster("cluster-b", "harbor.target.local/platform", "mgmt-regcred"),
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "mgmt-regcred", Namespace: ctrlcommon.ManagementNamespace()},
					Type:       corev1.SecretTypeDockerConfigJson,
					Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{"harbor.target.local":{"auth":"dGVzdA=="}}}`)},
				},
			).
			Build(),
		Scheme:              s,
		TargetClientFactory: &ctrlcommon.MockClientFactory{MockClient: targetClient},
	}

	ds := &disasterv1.DataSync{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"}}
	instance := &disasterv1.DisasterInstance{
		Spec:   disasterv1.DisasterInstanceSpec{Namespaces: []string{"app-ns"}},
		Status: disasterv1.DisasterInstanceStatus{PrimaryCluster: "cluster-a", SecondaryCluster: "cluster-b"},
	}

	spec, _, err := reconciler.buildAppRestoreSpec(ctx, ds, trafficlessConfig("cluster-b"), instance, "backup-001")
	if err != nil {
		t.Fatalf("buildAppRestoreSpec returned error: %v", err)
	}
	if got := trafficlessPatchValue(spec.ResourceModifierRules, "/spec/containers/0/image"); got != expectedTargetTrafficlessImage {
		t.Fatalf("expected target registry image, got %q", got)
	}
	if got := trafficlessPatchValue(spec.ResourceModifierRules, "/spec/imagePullSecrets"); got != `[{"name":"velero-regcred-cluster-b"}]` {
		t.Fatalf("expected imagePullSecrets patch, got %q", got)
	}

	secret := &corev1.Secret{}
	if err := targetClient.Get(ctx, types.NamespacedName{Name: "velero-regcred-cluster-b", Namespace: "app-ns"}, secret); err != nil {
		t.Fatalf("expected target namespace pull secret: %v", err)
	}
	if secret.Type != corev1.SecretTypeDockerConfigJson {
		t.Fatalf("unexpected secret type: %s", secret.Type)
	}
	if got := string(secret.Data[corev1.DockerConfigJsonKey]); got == "" {
		t.Fatalf("expected dockerconfigjson data to be synced")
	}
}

func TestBuildAppRestoreSpec_ExplicitTrafficlessImageIsNotRewrittenByTargetRegistry(t *testing.T) {
	ctx := context.Background()
	s := newDataSyncTrafficlessScheme(t)
	reconciler := &DataSyncReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(trafficlessCluster("cluster-b", "harbor.target.local/platform", "")).
			Build(),
		Scheme: s,
	}

	ds := &disasterv1.DataSync{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"},
		Spec: disasterv1.DataSyncSpec{
			TrafficlessConfig: &disasterv1.TrafficlessConfig{Image: "registry.local/tools/trafficless:v2"},
		},
	}
	instance := &disasterv1.DisasterInstance{
		Spec:   disasterv1.DisasterInstanceSpec{Namespaces: []string{"app-ns"}},
		Status: disasterv1.DisasterInstanceStatus{PrimaryCluster: "cluster-a", SecondaryCluster: "cluster-b"},
	}

	spec, _, err := reconciler.buildAppRestoreSpec(ctx, ds, trafficlessConfig("cluster-b"), instance, "backup-001")
	if err != nil {
		t.Fatalf("buildAppRestoreSpec returned error: %v", err)
	}
	if got := trafficlessPatchValue(spec.ResourceModifierRules, "/spec/containers/0/image"); got != "registry.local/tools/trafficless:v2" {
		t.Fatalf("expected explicit trafficless image to be preserved, got %q", got)
	}
}

func newDataSyncTrafficlessScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func trafficlessCluster(name, registry, credentialSecret string) *disasterv1.Cluster {
	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: disasterv1.ClusterSpec{
			VeleroInstall: &disasterv1.VeleroInstallSpec{ImageRegistry: registry},
		},
	}
	if credentialSecret != "" {
		cluster.Spec.VeleroInstall.RegistryCredentialSecretRef = &corev1.LocalObjectReference{Name: credentialSecret}
	}
	return cluster
}

func trafficlessConfig(target string) *disasterv1.DisasterConfig {
	return &disasterv1.DisasterConfig{
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "cluster-a",
			TargetCluster:     target,
			StorageRepository: "repo-main",
		},
	}
}

func trafficlessPatchValue(rules []disasterv1.ResourceModifierRule, path string) string {
	for _, rule := range rules {
		if rule.Conditions.GroupResource != "pods" {
			continue
		}
		for _, patch := range rule.Patches {
			if patch.Path == path {
				return patch.Value
			}
		}
	}
	return ""
}
