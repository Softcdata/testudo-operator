package disasteroperation

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestExecuteDrillRestoreResource_InheritsInstanceDynamicImageRewriteForInitContainers(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add kubernetes scheme: %v", err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}

	enabled := true
	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "inst-a", Namespace: "default"},
		Spec: disasterv1.DisasterInstanceSpec{
			Config:     "config-a",
			Namespaces: []string{"blueking"},
			RestorePolicy: &disasterv1.RestorePolicy{
				UseUnifiedDirectionResolver: &enabled,
				BulkModifierActions: []disasterv1.BulkModifierAction{{
					ID:              "rewrite-primary-registry",
					Action:          disasterv1.BulkModifierActionRewriteImage,
					Enabled:         &enabled,
					ApplyTo:         []disasterv1.RestoreModifierApplyTarget{disasterv1.RestoreModifierApplyDrill},
					DirectionPolicy: disasterv1.RestoreModifierDirectionPolicyAuto,
					ImageRewrite: &disasterv1.DynamicImageRewriteConfig{
						SourcePrefix:    "10.134.81.9:5000/",
						TargetPrefix:    "registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/",
						UnmatchedPolicy: disasterv1.ImageRewriteUnmatchedPolicyKeep,
					},
				}},
			},
		},
		Status: disasterv1.DisasterInstanceStatus{
			PrimaryCluster:   "cluster-a",
			SecondaryCluster: "cluster-b",
			ResourceSyncName: "rs-inst-a",
		},
	}
	config := &disasterv1.DisasterConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "config-a"},
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:     "cluster-a",
			TargetCluster:     "cluster-b",
			StorageRepository: "repo-main",
		},
	}
	resourceSync := &disasterv1.ResourceSync{
		ObjectMeta: metav1.ObjectMeta{Name: "rs-inst-a", Namespace: "default"},
		Status:     disasterv1.ResourceSyncStatus{LastBackupName: "resource-backup-001"},
	}
	operation := &disasterv1.DisasterOperation{
		ObjectMeta: metav1.ObjectMeta{Name: "drill-op", Namespace: "default"},
	}
	sourceCluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"},
		Spec:       disasterv1.ClusterSpec{Token: "token-a", Endpoint: "https://127.0.0.1:6443"},
	}
	targetCluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-b"},
		Spec:       disasterv1.ClusterSpec{Token: "token-b", Endpoint: "https://127.0.0.1:6443"},
	}

	managementClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(instance, config, resourceSync, operation, sourceCluster, targetCluster).
		WithStatusSubresource(operation).
		Build()
	sourceClient := fake.NewClientBuilder().WithScheme(s).WithObjects(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "bk-apigateway", Namespace: "blueking"},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "api", Image: "10.134.81.9:5000/blueking/bk-apigateway:v1"}},
			InitContainers: []corev1.Container{
				{Name: "wait-storages", Image: "10.134.81.9:5000/groundnuty/k8s-wait-for:v1.5.1"},
				{Name: "bk-apigateway-operator", Image: "10.134.81.9:5000/groundnuty/k8s-wait-for:v1.5.1"},
			},
		}}},
	}).Build()
	r := &DisasterOperationReconciler{
		Client:   managementClient,
		Scheme:   s,
		Log:      logr.Discard(),
		Recorder: record.NewFakeRecorder(16),
		ClientFactory: func(_ *rest.Config, _ ctrlclient.Options) (ctrlclient.Client, error) {
			return sourceClient, nil
		},
	}

	finished, err := r.executeDrillRestoreResource(ctx, logr.Discard(), instance, operation, "cluster-b")
	if err != nil {
		t.Fatalf("execute drill resource restore: %v", err)
	}
	if finished {
		t.Fatal("expected initial drill restore creation to remain in progress")
	}

	restores := &disasterv1.AppRestoreList{}
	if err := managementClient.List(ctx, restores, ctrlclient.InNamespace("default")); err != nil {
		t.Fatalf("list drill AppRestores: %v", err)
	}
	if len(restores.Items) != 1 {
		t.Fatalf("expected one drill AppRestore, got %d", len(restores.Items))
	}

	const targetImage = "registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/groundnuty/k8s-wait-for:v1.5.1"
	if got, ok := drillImageRewritePatchValue(restores.Items[0].Spec.ResourceModifierRules, "/spec/template/spec/initContainers/0/image"); !ok || got != targetImage {
		t.Fatalf("unexpected first initContainer rewrite target: %q", got)
	}
	if got, ok := drillImageRewritePatchValue(restores.Items[0].Spec.ResourceModifierRules, "/spec/template/spec/initContainers/1/image"); !ok || got != targetImage {
		t.Fatalf("unexpected second initContainer rewrite target: %q", got)
	}
}

func drillImageRewritePatchValue(rules []disasterv1.ResourceModifierRule, path string) (string, bool) {
	for _, rule := range rules {
		for _, patch := range rule.Patches {
			if patch.Path == path {
				return patch.Value, true
			}
		}
	}
	return "", false
}
