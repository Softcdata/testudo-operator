package disasterinstance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/softcdata/testudo-operator/internal/controller/restore"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

type fakeRuleResourceLocator struct {
	byGroupResource map[string][]unstructured.Unstructured
}

func (f *fakeRuleResourceLocator) ListMatchingResources(
	_ context.Context,
	conditions disasterv1.Conditions,
	_ []string,
) ([]unstructured.Unstructured, error) {
	items := f.byGroupResource[strings.TrimSpace(conditions.GroupResource)]
	out := make([]unstructured.Unstructured, 0, len(items))
	for _, item := range items {
		out = append(out, *item.DeepCopy())
	}
	return out, nil
}

func TestRestorePolicyValidatorHandle_AllowValidModifierRule(t *testing.T) {
	t.Parallel()

	sch := runtime.NewScheme()
	if err := disasterv1.AddToScheme(sch); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}
	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(
		&disasterv1.DisasterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cfg-a"},
			Spec: disasterv1.DisasterConfigSpec{
				SourceCluster: "cluster-a",
				TargetCluster: "cluster-b",
			},
		},
		&disasterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"},
			Spec: disasterv1.ClusterSpec{
				KubeConfig: []byte("fake"),
			},
		},
	).Build()

	validator := NewRestorePolicyValidator(cli)
	validator.ClusterRESTConfigFunc = func(*disasterv1.Cluster) (*rest.Config, error) {
		return &rest.Config{Host: "https://cluster-a.example"}, nil
	}
	validator.LocatorBuilderFunc = func(*rest.Config) (restore.ModifierRuleResourceLocator, error) {
		return &fakeRuleResourceLocator{
			byGroupResource: map[string][]unstructured.Unstructured{
				"services": {
					{
						Object: map[string]any{
							"metadata": map[string]any{"name": "svc-1", "namespace": "prod"},
							"spec": map[string]any{
								"ports": []any{
									map[string]any{"nodePort": int64(30080)},
								},
							},
						},
					},
				},
			},
		}, nil
	}

	instance := baseDisasterInstanceForWebhook(disasterv1.RestoreModifierRule{
		ID:   "svc-nodeport",
		Mode: disasterv1.RestoreModifierModeReversible,
		Conditions: disasterv1.Conditions{
			GroupResource: "services",
		},
		Pair: &disasterv1.RestoreModifierPair{
			Path:        "/spec/ports/0/nodePort",
			SourceValue: "30080",
			TargetValue: "32080",
		},
	})

	raw, err := json.Marshal(instance)
	if err != nil {
		t.Fatalf("marshal instance: %v", err)
	}
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Fatalf("expected allowed response, got denied: %s", resp.Result.Message)
	}
}

func TestRestorePolicyValidatorHandle_DenyArrayOutOfBounds(t *testing.T) {
	t.Parallel()

	sch := runtime.NewScheme()
	if err := disasterv1.AddToScheme(sch); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}
	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(
		&disasterv1.DisasterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cfg-a"},
			Spec: disasterv1.DisasterConfigSpec{
				SourceCluster: "cluster-a",
				TargetCluster: "cluster-b",
			},
		},
		&disasterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"},
			Spec: disasterv1.ClusterSpec{
				KubeConfig: []byte("fake"),
			},
		},
	).Build()

	validator := NewRestorePolicyValidator(cli)
	validator.ClusterRESTConfigFunc = func(*disasterv1.Cluster) (*rest.Config, error) {
		return &rest.Config{Host: "https://cluster-a.example"}, nil
	}
	validator.LocatorBuilderFunc = func(*rest.Config) (restore.ModifierRuleResourceLocator, error) {
		return &fakeRuleResourceLocator{
			byGroupResource: map[string][]unstructured.Unstructured{
				"services": {
					{
						Object: map[string]any{
							"metadata": map[string]any{"name": "svc-1", "namespace": "prod"},
							"spec": map[string]any{
								"ports": []any{
									map[string]any{"nodePort": int64(30080)},
								},
							},
						},
					},
				},
			},
		}, nil
	}

	instance := baseDisasterInstanceForWebhook(disasterv1.RestoreModifierRule{
		ID:   "svc-nodeport",
		Mode: disasterv1.RestoreModifierModeReversible,
		Conditions: disasterv1.Conditions{
			GroupResource: "services",
		},
		Pair: &disasterv1.RestoreModifierPair{
			Path:        "/spec/ports/10/nodePort",
			SourceValue: "30080",
			TargetValue: "32080",
		},
	})

	raw, err := json.Marshal(instance)
	if err != nil {
		t.Fatalf("marshal instance: %v", err)
	}
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if resp.Allowed {
		t.Fatalf("expected denied response")
	}
	if !strings.Contains(resp.Result.Message, restore.ModifierErrorRuleRejected) {
		t.Fatalf("expected %s in deny message, got %s", restore.ModifierErrorRuleRejected, resp.Result.Message)
	}
}

func TestRestorePolicyValidatorHandle_DenyZeroMatch(t *testing.T) {
	t.Parallel()

	sch := runtime.NewScheme()
	if err := disasterv1.AddToScheme(sch); err != nil {
		t.Fatalf("add disaster scheme: %v", err)
	}
	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(
		&disasterv1.DisasterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cfg-a"},
			Spec: disasterv1.DisasterConfigSpec{
				SourceCluster: "cluster-a",
				TargetCluster: "cluster-b",
			},
		},
		&disasterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"},
			Spec: disasterv1.ClusterSpec{
				KubeConfig: []byte("fake"),
			},
		},
	).Build()

	validator := NewRestorePolicyValidator(cli)
	validator.ClusterRESTConfigFunc = func(*disasterv1.Cluster) (*rest.Config, error) {
		return &rest.Config{Host: "https://cluster-a.example"}, nil
	}
	validator.LocatorBuilderFunc = func(*rest.Config) (restore.ModifierRuleResourceLocator, error) {
		return &fakeRuleResourceLocator{
			byGroupResource: map[string][]unstructured.Unstructured{
				"deployments.apps": {},
			},
		}, nil
	}

	instance := baseDisasterInstanceForWebhook(disasterv1.RestoreModifierRule{
		ID:   "no-hit",
		Mode: disasterv1.RestoreModifierModeVeleroNative,
		Conditions: disasterv1.Conditions{
			GroupResource: "deployments.apps",
		},
		VeleroRule: &disasterv1.RestoreModifierVeleroRule{
			Patches: []disasterv1.JSONPatch{{
				Operation: "add",
				Path:      "/metadata/annotations/patched-by",
				Value:     "platform",
			}},
		},
	})

	raw, err := json.Marshal(instance)
	if err != nil {
		t.Fatalf("marshal instance: %v", err)
	}
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if resp.Allowed {
		t.Fatalf("expected denied response")
	}
	if !strings.Contains(resp.Result.Message, "matched zero resources") {
		t.Fatalf("expected zero-match message, got %s", resp.Result.Message)
	}
}

func baseDisasterInstanceForWebhook(rule disasterv1.RestoreModifierRule) *disasterv1.DisasterInstance {
	return &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "instance-a",
			Namespace: "default",
		},
		Spec: disasterv1.DisasterInstanceSpec{
			Config:     "cfg-a",
			Namespaces: []string{"prod"},
			RestorePolicy: &disasterv1.RestorePolicy{
				UseUnifiedDirectionResolver: boolPtr(true),
				ModifierRules:               []disasterv1.RestoreModifierRule{rule},
			},
		},
	}
}

func boolPtr(v bool) *bool { return &v }

func TestRestorePolicyValidatorHandle_DenyResourceSelectionScopedConflict(t *testing.T) {
	t.Parallel()

	validator := NewRestorePolicyValidator(nil)
	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "instance-scoped-conflict", Namespace: "default"},
		Spec: disasterv1.DisasterInstanceSpec{
			RestorePolicy: &disasterv1.RestorePolicy{
				ResourceSelection: &disasterv1.RestoreResourceSelectionPolicy{
					IncludedNamespaceScopedResources: []string{"deployments.apps"},
					ExcludedNamespaceScopedResources: []string{"deployments.apps"},
				},
			},
		},
	}

	raw, err := json.Marshal(instance)
	if err != nil {
		t.Fatalf("marshal instance: %v", err)
	}
	resp := validator.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: raw},
		},
	})

	if resp.Allowed {
		t.Fatalf("expected denied response")
	}
	if !strings.Contains(resp.Result.Message, restore.ResourceSelectionErrorInvalid) {
		t.Fatalf("expected %s in deny message, got %s", restore.ResourceSelectionErrorInvalid, resp.Result.Message)
	}
	if !strings.Contains(resp.Result.Message, "includedNamespaceScopedResources") {
		t.Fatalf("expected scoped field path in deny message, got %s", resp.Result.Message)
	}
}

func TestRestorePolicyValidatorHandle_AllowWhenIncludeClusterResourcesTrueSkipsScopedConflict(t *testing.T) {
	t.Parallel()

	validator := NewRestorePolicyValidator(nil)
	includeClusterResources := true
	instance := &disasterv1.DisasterInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "instance-old-priority", Namespace: "default"},
		Spec: disasterv1.DisasterInstanceSpec{
			RestorePolicy: &disasterv1.RestorePolicy{
				ResourceSelection: &disasterv1.RestoreResourceSelectionPolicy{
					IncludeClusterResources:          &includeClusterResources,
					IncludedResources:                []string{"deployments.apps"},
					ExcludedResources:                []string{"secrets"},
					IncludedNamespaceScopedResources: []string{"services"},
					ExcludedNamespaceScopedResources: []string{"services"},
				},
			},
		},
	}

	raw, err := json.Marshal(instance)
	if err != nil {
		t.Fatalf("marshal instance: %v", err)
	}
	resp := validator.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: raw},
		},
	})

	if !resp.Allowed {
		t.Fatalf("expected allowed response, got denied: %s", resp.Result.Message)
	}
}
