package disasteroperation

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/go-logr/logr"
	ctrlcommon "github.com/softcdata/testudo-operator/internal/controller"
	restorebuilder "github.com/softcdata/testudo-operator/internal/controller/restore"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const drillPatchOperationAdd = "add"

func TestResolveDrillDataRestoreHooks(t *testing.T) {
	instanceHooks := &velerov1.RestoreHooks{Resources: []velerov1.RestoreResourceHookSpec{{Name: "instance"}}}
	drillHooks := &velerov1.RestoreHooks{Resources: []velerov1.RestoreResourceHookSpec{{Name: "drill"}}}

	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			VeleroHooks: &disasterv1.DisasterVeleroHooks{DataRestore: instanceHooks},
		},
	}

	if got := resolveDrillDataRestoreHooks(instance, nil); !reflect.DeepEqual(got, instanceHooks) {
		t.Fatalf("expected instance hooks, got %#v", got)
	}
	if got := resolveDrillDataRestoreHooks(instance, &disasterv1.DrillConfig{VeleroHooks: &disasterv1.DisasterVeleroHooks{DataRestore: drillHooks}}); !reflect.DeepEqual(got, drillHooks) {
		t.Fatalf("expected drill hooks, got %#v", got)
	}
	if got := resolveDrillDataRestoreHooks(instance, &disasterv1.DrillConfig{VeleroHooks: &disasterv1.DisasterVeleroHooks{}}); got != nil {
		t.Fatalf("expected drill empty hooks to clear inherited hooks, got %#v", got)
	}
}

func TestExecuteDrillRestoreData_RewritesDataRestoreHookForTrafficless(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	hooks := &velerov1.RestoreHooks{
		Resources: []velerov1.RestoreResourceHookSpec{{
			Name:               "drill-post",
			IncludedNamespaces: []string{"app-ns"},
			IncludedResources:  []string{"pods"},
			LabelSelector:      &metav1.LabelSelector{MatchLabels: map[string]string{"app": "hook-di"}},
			PostHooks: []velerov1.RestoreResourceHook{{
				Exec: &velerov1.ExecRestoreHook{
					Container:   "app",
					Command:     []string{"/usr/local/bin/dr-hook", "after-restore"},
					ExecTimeout: metav1.Duration{Duration: time.Minute},
				},
			}},
		}},
	}
	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "inst-a", Namespace: "default"},
		Spec: disasterv1.DisasterInstanceSpec{
			Config:     "config-a",
			Namespaces: []string{"app-ns"},
			VeleroHooks: &disasterv1.DisasterVeleroHooks{
				DataRestore: hooks,
			},
		},
		Status: disasterv1.DisasterInstanceStatus{
			PrimaryCluster: "cluster-a",
			DataSyncName:   "ds-inst-a",
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
	dataSync := &disasterv1.DataSync{
		ObjectMeta: metav1.ObjectMeta{Name: "ds-inst-a", Namespace: "default"},
		Spec: disasterv1.DataSyncSpec{
			TrafficlessConfig: &disasterv1.TrafficlessConfig{
				Image:   "registry.local/rongzai/busybox:1.36",
				Command: []string{"sleep", "3600"},
			},
		},
		Status: disasterv1.DataSyncStatus{
			LastBackupName: "backup-001",
		},
	}
	operation := &disasterv1.DisasterOperation{
		ObjectMeta: metav1.ObjectMeta{Name: "drill-op", Namespace: "default"},
		Spec: disasterv1.DisasterOperationSpec{
			DrillConfig: &disasterv1.DrillConfig{
				NamespaceMapping: map[string]string{"app-ns": "drill-ns"},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(instance, config, dataSync, operation).
		WithStatusSubresource(operation).
		Build()
	r := &DisasterOperationReconciler{
		Client:   c,
		Scheme:   s,
		Log:      logr.Discard(),
		Recorder: record.NewFakeRecorder(16),
	}

	finished, err := r.executeDrillRestoreData(ctx, logr.Discard(), instance, operation, "cluster-drill")
	if err != nil {
		t.Fatalf("executeDrillRestoreData returned error: %v", err)
	}
	if finished {
		t.Fatalf("expected first execution to create AppRestore and remain unfinished")
	}

	appRestore := &disasterv1.AppRestore{}
	if err := c.Get(ctx, types.NamespacedName{Name: "ddr-drill-op-904ce4", Namespace: "default"}, appRestore); err != nil {
		t.Fatal(err)
	}
	markerKey := restorebuilder.DataRestoreHookMarkerLabelKey(0)
	wantSelector := &metav1.LabelSelector{MatchLabels: map[string]string{markerKey: "true"}}
	if !reflect.DeepEqual(appRestore.Spec.Template.Hooks.Resources[0].LabelSelector, wantSelector) {
		t.Fatalf("expected drill data restore hook selector %#v, got %#v", wantSelector, appRestore.Spec.Template.Hooks.Resources[0].LabelSelector)
	}
	if !reflect.DeepEqual(appRestore.Spec.Template.Hooks.Resources[0].IncludedNamespaces, []string{"drill-ns"}) {
		t.Fatalf("expected exec hook namespace mapped to drill-ns, got %#v", appRestore.Spec.Template.Hooks.Resources[0].IncludedNamespaces)
	}
	if !hasDisasterOperationMarkerRule(appRestore.Spec.ResourceModifierRules, []string{"app-ns"}) {
		t.Fatalf("expected marker rule to match backup namespace app-ns, got %#v", appRestore.Spec.ResourceModifierRules)
	}
	if got, ok := disasterOperationPatchValue(appRestore.Spec.ResourceModifierRules, "/spec/containers/0/image"); !ok || got != "registry.local/rongzai/busybox:1.36" {
		t.Fatalf("expected drill trafficless image from DataSync config, got value=%q found=%v rules=%#v", got, ok, appRestore.Spec.ResourceModifierRules)
	}
	if got, ok := disasterOperationPatchValue(appRestore.Spec.ResourceModifierRules, "/spec/containers/0/command"); !ok || got != `["sleep","3600"]` {
		t.Fatalf("expected drill trafficless command from DataSync config, got value=%q found=%v rules=%#v", got, ok, appRestore.Spec.ResourceModifierRules)
	}
	if got, ok := disasterOperationPatchValue(appRestore.Spec.ResourceModifierRules, "/spec/containers/0/args"); !ok || got != "[]" {
		t.Fatalf("expected drill trafficless args to be cleared, got value=%q found=%v rules=%#v", got, ok, appRestore.Spec.ResourceModifierRules)
	}
	if patch := disasterOperationPatchByPath(appRestore.Spec.ResourceModifierRules, "/spec/nodeName"); patch == nil || patch.Operation != drillPatchOperationAdd || patch.Value != "" {
		t.Fatalf("expected drill trafficless nodeName cleanup patch, got %#v rules=%#v", patch, appRestore.Spec.ResourceModifierRules)
	}
	if patch := disasterOperationPatchByPath(appRestore.Spec.ResourceModifierRules, "/spec/nodeSelector"); patch == nil || patch.Operation != drillPatchOperationAdd || patch.Value != "{}" {
		t.Fatalf("expected drill trafficless nodeSelector cleanup patch, got %#v rules=%#v", patch, appRestore.Spec.ResourceModifierRules)
	}
	if patch := disasterOperationPatchByPath(appRestore.Spec.ResourceModifierRules, "/spec/affinity"); patch == nil || patch.Operation != drillPatchOperationAdd || patch.Value != "{}" {
		t.Fatalf("expected drill trafficless affinity cleanup patch, got %#v rules=%#v", patch, appRestore.Spec.ResourceModifierRules)
	}
}

func TestExecuteDrillRestoreData_UsesDrillTargetClusterTrafficlessRegistryAndMappedSecret(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "inst-a", Namespace: "default"},
		Spec: disasterv1.DisasterInstanceSpec{
			Config:     "config-a",
			Namespaces: []string{"app-ns"},
		},
		Status: disasterv1.DisasterInstanceStatus{
			PrimaryCluster: "cluster-a",
			DataSyncName:   "ds-inst-a",
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
	dataSync := &disasterv1.DataSync{
		ObjectMeta: metav1.ObjectMeta{Name: "ds-inst-a", Namespace: "default"},
		Status: disasterv1.DataSyncStatus{
			LastBackupName: "backup-001",
		},
	}
	operation := &disasterv1.DisasterOperation{
		ObjectMeta: metav1.ObjectMeta{Name: "drill-op", Namespace: "default"},
		Spec: disasterv1.DisasterOperationSpec{
			DrillConfig: &disasterv1.DrillConfig{
				NamespaceMapping: map[string]string{"app-ns": "drill-ns"},
			},
		},
	}
	targetCluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-drill"},
		Spec: disasterv1.ClusterSpec{
			Token:    "token",
			Endpoint: "https://127.0.0.1:6443",
			VeleroInstall: &disasterv1.VeleroInstallSpec{
				ImageRegistry: "harbor.dr.local/platform",
				RegistryCredentialSecretRef: &corev1.LocalObjectReference{
					Name: "mgmt-regcred",
				},
			},
		},
	}
	managementSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mgmt-regcred", Namespace: ctrlcommon.ManagementNamespace()},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{"harbor.dr.local":{"auth":"dGVzdA=="}}}`)},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(instance, config, dataSync, operation, targetCluster, managementSecret).
		WithStatusSubresource(operation).
		Build()
	targetClient := fake.NewClientBuilder().WithScheme(s).Build()
	r := &DisasterOperationReconciler{
		Client:   c,
		Scheme:   s,
		Log:      logr.Discard(),
		Recorder: record.NewFakeRecorder(16),
		ClientFactory: func(_ *rest.Config, _ ctrlclient.Options) (ctrlclient.Client, error) {
			return targetClient, nil
		},
	}

	finished, err := r.executeDrillRestoreData(ctx, logr.Discard(), instance, operation, "cluster-drill")
	if err != nil {
		t.Fatalf("executeDrillRestoreData returned error: %v", err)
	}
	if finished {
		t.Fatalf("expected first execution to create AppRestore and remain unfinished")
	}

	appRestore := &disasterv1.AppRestore{}
	if err := c.Get(ctx, types.NamespacedName{Name: "ddr-drill-op-904ce4", Namespace: "default"}, appRestore); err != nil {
		t.Fatal(err)
	}
	if got, ok := disasterOperationPatchValue(appRestore.Spec.ResourceModifierRules, "/spec/containers/0/image"); !ok || got != "harbor.dr.local/platform/busybox:1.36" {
		t.Fatalf("expected drill target cluster trafficless image, got value=%q found=%v rules=%#v", got, ok, appRestore.Spec.ResourceModifierRules)
	}
	if got, ok := disasterOperationPatchValue(appRestore.Spec.ResourceModifierRules, "/spec/imagePullSecrets"); !ok || got != `[{"name":"velero-regcred-cluster-drill"}]` {
		t.Fatalf("expected drill imagePullSecrets patch, got value=%q found=%v rules=%#v", got, ok, appRestore.Spec.ResourceModifierRules)
	}
	if patch := disasterOperationPatchByPath(appRestore.Spec.ResourceModifierRules, "/spec/nodeName"); patch == nil || patch.Operation != drillPatchOperationAdd || patch.Value != "" {
		t.Fatalf("expected drill trafficless nodeName cleanup patch with target registry modifier, got %#v rules=%#v", patch, appRestore.Spec.ResourceModifierRules)
	}
	if patch := disasterOperationPatchByPath(appRestore.Spec.ResourceModifierRules, "/spec/nodeSelector"); patch == nil || patch.Operation != drillPatchOperationAdd || patch.Value != "{}" {
		t.Fatalf("expected drill trafficless nodeSelector cleanup patch with target registry modifier, got %#v rules=%#v", patch, appRestore.Spec.ResourceModifierRules)
	}
	if patch := disasterOperationPatchByPath(appRestore.Spec.ResourceModifierRules, "/spec/affinity"); patch == nil || patch.Operation != drillPatchOperationAdd || patch.Value != "{}" {
		t.Fatalf("expected drill trafficless affinity cleanup patch with target registry modifier, got %#v rules=%#v", patch, appRestore.Spec.ResourceModifierRules)
	}

	secret := &corev1.Secret{}
	if err := targetClient.Get(ctx, types.NamespacedName{Name: "velero-regcred-cluster-drill", Namespace: "drill-ns"}, secret); err != nil {
		t.Fatalf("expected mapped drill namespace pull secret: %v", err)
	}
	if err := targetClient.Get(ctx, types.NamespacedName{Name: "velero-regcred-cluster-drill", Namespace: "app-ns"}, &corev1.Secret{}); err == nil {
		t.Fatalf("did not expect pull secret in source namespace when drill namespaceMapping redirects it")
	}
}

func hasDisasterOperationMarkerRule(rules []disasterv1.ResourceModifierRule, namespaces []string) bool {
	for _, rule := range rules {
		if rule.Conditions.GroupResource != "pods" {
			continue
		}
		if !reflect.DeepEqual(rule.Conditions.Namespaces, namespaces) {
			continue
		}
		for _, patch := range rule.Patches {
			if patch.Path == "/metadata/labels/testudo.softcdata.com~1data-restore-hook-0" && patch.Value == `"true"` {
				return true
			}
		}
	}
	return false
}

func disasterOperationPatchValue(rules []disasterv1.ResourceModifierRule, path string) (string, bool) {
	patch := disasterOperationPatchByPath(rules, path)
	if patch == nil {
		return "", false
	}
	return patch.Value, true
}

func disasterOperationPatchByPath(rules []disasterv1.ResourceModifierRule, path string) *disasterv1.JSONPatch {
	for _, rule := range rules {
		for i := range rule.Patches {
			if rule.Patches[i].Path == path {
				return &rule.Patches[i]
			}
		}
	}
	return nil
}
