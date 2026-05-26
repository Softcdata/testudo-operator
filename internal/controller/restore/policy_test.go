package restore

import (
	"context"
	"strings"
	"testing"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
)

func TestApplyInstanceRestorePolicy_AppliesTemplateAndClassRules(t *testing.T) {
	includeClusterResources := true
	restorePVs := false

	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"demo"},
			RestorePolicy: &disasterv1.RestorePolicy{
				ResourceSelection: &disasterv1.RestoreResourceSelectionPolicy{
					IncludedResources:       []string{"deployments", "services"},
					ExcludedResources:       []string{"secrets"},
					IncludeClusterResources: &includeClusterResources,
				},
				Execution: &disasterv1.RestoreExecutionPolicy{
					ExistingResourcePolicy: "update",
					RestorePVs:             &restorePVs,
				},
				StorageClassMapping: &disasterv1.RestoreClassMappingPolicy{
					Mappings: []disasterv1.RestoreClassMapping{{
						SourceClass: "standard",
						TargetClass: "fast",
					}},
				},
				IngressClassMapping: &disasterv1.RestoreClassMappingPolicy{
					Mappings: []disasterv1.RestoreClassMapping{{
						SourceClass: "nginx",
						TargetClass: "traefik",
					}},
				},
			},
		},
	}

	spec := &disasterv1.AppRestoreSpec{
		Template: velerov1.RestoreSpec{},
	}

	summary, err := ApplyInstanceRestorePolicy(context.Background(), spec, instance, nil)
	if err != nil {
		t.Fatalf("ApplyInstanceRestorePolicy returned error: %v", err)
	}

	if summary.Source != "instance" {
		t.Fatalf("expected summary source instance, got %s", summary.Source)
	}
	if spec.Template.ExistingResourcePolicy != velerov1.PolicyTypeUpdate {
		t.Fatalf("expected existingResourcePolicy=update, got %s", spec.Template.ExistingResourcePolicy)
	}
	if spec.Template.RestorePVs == nil || *spec.Template.RestorePVs != restorePVs {
		t.Fatalf("expected RestorePVs=%v", restorePVs)
	}
	if len(spec.Template.IncludedResources) != 2 {
		t.Fatalf("expected included resources overridden by policy")
	}
	if len(spec.ResourceModifierRules) != 3 {
		t.Fatalf("expected 3 mapping rules (pvc+pv+ingress), got %d", len(spec.ResourceModifierRules))
	}
}

func TestApplyInstanceRestorePolicy_NoPolicyKeepsDefaults(t *testing.T) {
	originalRestorePVs := true
	spec := &disasterv1.AppRestoreSpec{
		Template: velerov1.RestoreSpec{
			BackupName: "backup-001",
			RestorePVs: &originalRestorePVs,
		},
	}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"demo"},
		},
	}

	summary, err := ApplyInstanceRestorePolicy(context.Background(), spec, instance, nil)
	if err != nil {
		t.Fatalf("expected no error when restorePolicy is nil, got %v", err)
	}
	if summary.Source != "default" {
		t.Fatalf("expected source=default, got %s", summary.Source)
	}
	if spec.Template.BackupName != "backup-001" {
		t.Fatalf("expected backup name to keep default value")
	}
	if spec.Template.RestorePVs == nil || *spec.Template.RestorePVs != originalRestorePVs {
		t.Fatalf("expected RestorePVs to keep default value")
	}
	if len(spec.ResourceModifierRules) != 0 {
		t.Fatalf("expected no extra modifier rules when restorePolicy is nil")
	}
}

func TestApplyInstanceRestorePolicy_DrillOverrideReplacesModifierInputsButKeepsClassMapping(t *testing.T) {
	instance := &disasterv1.DisasterInstance{
		Status: disasterv1.DisasterInstanceStatus{
			PrimaryCluster:   "cluster-a",
			SecondaryCluster: "cluster-b",
		},
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"demo"},
			RestorePolicy: &disasterv1.RestorePolicy{
				StorageClassMapping: &disasterv1.RestoreClassMappingPolicy{
					Mappings: []disasterv1.RestoreClassMapping{{
						SourceClass: "standard",
						TargetClass: "fast",
					}},
				},
				UseUnifiedDirectionResolver: boolPtr(true),
				ModifierRules: []disasterv1.RestoreModifierRule{{
					ID:   "instance-rule",
					Mode: disasterv1.RestoreModifierModeReversible,
					ApplyTo: []disasterv1.RestoreModifierApplyTarget{
						disasterv1.RestoreModifierApplyDrill,
					},
					Conditions: disasterv1.Conditions{
						GroupResource: "deployments.apps",
						Namespaces:    []string{"demo"},
					},
					Pair: &disasterv1.RestoreModifierPair{
						Path:        "/metadata/annotations/instance-only",
						SourceValue: "src",
						TargetValue: "dst",
					},
				}},
			},
		},
	}
	override := &disasterv1.RestorePolicy{
		UseUnifiedDirectionResolver: boolPtr(true),
		ModifierRules: []disasterv1.RestoreModifierRule{{
			ID:   "drill-rule",
			Mode: disasterv1.RestoreModifierModeReversible,
			ApplyTo: []disasterv1.RestoreModifierApplyTarget{
				disasterv1.RestoreModifierApplyDrill,
			},
			Conditions: disasterv1.Conditions{
				GroupResource: "deployments.apps",
				Namespaces:    []string{"demo"},
			},
			Pair: &disasterv1.RestoreModifierPair{
				Path:        "/metadata/annotations/drill-only",
				SourceValue: "from-a",
				TargetValue: "to-b",
			},
		}},
	}

	spec := &disasterv1.AppRestoreSpec{
		Template: velerov1.RestoreSpec{},
	}

	summary, err := ApplyInstanceRestorePolicy(
		context.Background(),
		spec,
		instance,
		nil,
		WithBaselineClusters("cluster-a", "cluster-b"),
		WithApplyTarget(disasterv1.RestoreModifierApplyDrill),
		WithRestorePolicyOverride(override),
	)
	if err != nil {
		t.Fatalf("ApplyInstanceRestorePolicy returned error: %v", err)
	}

	if summary.Source != "drillOverride" {
		t.Fatalf("expected summary source drillOverride, got %s", summary.Source)
	}
	if len(spec.ResourceModifierRules) != 3 {
		t.Fatalf("expected pvc+pv storage mapping plus drill override rule, got %d", len(spec.ResourceModifierRules))
	}
	foundDrillRule := false
	for _, rule := range spec.ResourceModifierRules {
		if len(rule.Patches) == 0 {
			continue
		}
		patch := rule.Patches[0]
		if patch.Path != "/metadata/annotations/drill-only" {
			continue
		}
		foundDrillRule = true
		if strings.Contains(patch.Value, "instance-only") {
			t.Fatalf("expected instance modifier to be replaced, got %s", patch.Value)
		}
		if !strings.Contains(patch.Value, "to-b") {
			t.Fatalf("expected drill override patch value to be compiled, got %s", patch.Value)
		}
	}
	if !foundDrillRule {
		t.Fatalf("expected drill override patch to be present")
	}
}

func TestApplyInstanceRestorePolicy_RejectsConflictingClassMappings(t *testing.T) {
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			RestorePolicy: &disasterv1.RestorePolicy{
				StorageClassMapping: &disasterv1.RestoreClassMappingPolicy{
					Mappings: []disasterv1.RestoreClassMapping{
						{SourceClass: "standard", TargetClass: "gold"},
						{SourceClass: "standard", TargetClass: "silver"},
					},
				},
			},
		},
	}
	spec := &disasterv1.AppRestoreSpec{Template: velerov1.RestoreSpec{}}
	_, err := ApplyInstanceRestorePolicy(context.Background(), spec, instance, nil)
	if err == nil {
		t.Fatalf("expected class mapping conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "ClassMappingInvalid") {
		t.Fatalf("expected ClassMappingInvalid error, got %v", err)
	}
}

func TestApplyInstanceRestorePolicy_StrictValidationFailsWhenStorageClassMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := storagev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add storage scheme: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add networking scheme: %v", err)
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			RestorePolicy: &disasterv1.RestorePolicy{
				StorageClassMapping: &disasterv1.RestoreClassMappingPolicy{
					StrictTargetValidation: true,
					Mappings: []disasterv1.RestoreClassMapping{{
						SourceClass: "standard",
						TargetClass: "gold",
					}},
				},
			},
		},
	}

	spec := &disasterv1.AppRestoreSpec{Template: velerov1.RestoreSpec{}}
	_, err := ApplyInstanceRestorePolicy(context.Background(), spec, instance, fakeClient)
	if err == nil {
		t.Fatalf("expected strict validation error, got nil")
	}
	if !strings.Contains(err.Error(), "StorageClassTargetNotFound") {
		t.Fatalf("expected StorageClassTargetNotFound, got %v", err)
	}
}

func TestApplyInstanceRestorePolicy_StrictValidationPassesWhenClassesExist(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := storagev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add storage scheme: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add networking scheme: %v", err)
	}

	sc := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "gold"}}
	ic := &networkingv1.IngressClass{ObjectMeta: metav1.ObjectMeta{Name: "traefik"}}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sc, ic).Build()

	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"demo"},
			RestorePolicy: &disasterv1.RestorePolicy{
				StorageClassMapping: &disasterv1.RestoreClassMappingPolicy{
					StrictTargetValidation: true,
					Mappings: []disasterv1.RestoreClassMapping{{
						SourceClass: "standard",
						TargetClass: "gold",
					}},
				},
				IngressClassMapping: &disasterv1.RestoreClassMappingPolicy{
					StrictTargetValidation: true,
					Mappings: []disasterv1.RestoreClassMapping{{
						SourceClass: "nginx",
						TargetClass: "traefik",
					}},
				},
			},
		},
	}

	spec := &disasterv1.AppRestoreSpec{Template: velerov1.RestoreSpec{}}
	summary, err := ApplyInstanceRestorePolicy(context.Background(), spec, instance, fakeClient)
	if err != nil {
		t.Fatalf("expected no strict validation error, got %v", err)
	}
	if !summary.StorageStrictValidation || !summary.IngressStrictValidation {
		t.Fatalf("expected strict validation flags in summary")
	}
}

func TestApplyInstanceRestorePolicy_StrictValidationDoesNotAutoReverseStorageClassMapping(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := storagev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add storage scheme: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add networking scheme: %v", err)
	}

	// 目标集群只有 sourceClass，不存在 targetClass。
	// strictTargetValidation 语义：必须按显式 targetClass 严格校验，不能自动反转放行。
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "sc-main"}},
	).Build()

	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"demo"},
			RestorePolicy: &disasterv1.RestorePolicy{
				StorageClassMapping: &disasterv1.RestoreClassMappingPolicy{
					StrictTargetValidation: true,
					Mappings: []disasterv1.RestoreClassMapping{{
						SourceClass: "sc-main",
						TargetClass: "sc-dr",
					}},
				},
			},
		},
	}

	spec := &disasterv1.AppRestoreSpec{Template: velerov1.RestoreSpec{}}
	_, err := ApplyInstanceRestorePolicy(context.Background(), spec, instance, fakeClient)
	if err == nil {
		t.Fatalf("expected strict validation failure when target class is missing")
	}
	if !strings.Contains(err.Error(), "StorageClassTargetNotFound") {
		t.Fatalf("expected StorageClassTargetNotFound, got %v", err)
	}
	if got := len(spec.ResourceModifierRules); got != 0 {
		t.Fatalf("expected no generated mapping rules on strict validation failure, got %d", got)
	}
}

func TestApplyInstanceRestorePolicy_StrictValidationDoesNotAutoReverseIngressClassMapping(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := storagev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add storage scheme: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add networking scheme: %v", err)
	}

	// 目标集群仅存在 source ingress class，不存在显式 target ingress class。
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&networkingv1.IngressClass{ObjectMeta: metav1.ObjectMeta{Name: "ing-main"}},
	).Build()

	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"demo"},
			RestorePolicy: &disasterv1.RestorePolicy{
				IngressClassMapping: &disasterv1.RestoreClassMappingPolicy{
					StrictTargetValidation: true,
					Mappings: []disasterv1.RestoreClassMapping{{
						SourceClass: "ing-main",
						TargetClass: "ing-dr",
					}},
				},
			},
		},
	}

	spec := &disasterv1.AppRestoreSpec{Template: velerov1.RestoreSpec{}}
	_, err := ApplyInstanceRestorePolicy(context.Background(), spec, instance, fakeClient)
	if err == nil {
		t.Fatalf("expected strict validation failure when target ingress class is missing")
	}
	if !strings.Contains(err.Error(), "IngressClassTargetNotFound") {
		t.Fatalf("expected IngressClassTargetNotFound, got %v", err)
	}
	if got := len(spec.ResourceModifierRules); got != 0 {
		t.Fatalf("expected no generated mapping rules on strict validation failure, got %d", got)
	}
}

func TestApplyInstanceRestorePolicy_GateEnabledStrictValidationUsesDirectionWithoutLegacyFallback(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := storagev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add storage scheme: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add networking scheme: %v", err)
	}

	targetClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "sc-main"}}).
		Build()

	basePolicy := &disasterv1.RestorePolicy{
		UseUnifiedDirectionResolver: boolPtr(true),
		StorageClassMapping: &disasterv1.RestoreClassMappingPolicy{
			StrictTargetValidation: true,
			Mappings: []disasterv1.RestoreClassMapping{{
				SourceClass: "sc-main",
				TargetClass: "sc-dr",
			}},
		},
	}

	reverseInst := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces:    []string{"app-ns"},
			RestorePolicy: basePolicy.DeepCopy(),
		},
		Status: disasterv1.DisasterInstanceStatus{
			PrimaryCluster:   "cluster-b",
			SecondaryCluster: "cluster-a",
		},
	}
	reverseSpec := &disasterv1.AppRestoreSpec{Template: velerov1.RestoreSpec{}}
	_, err := ApplyInstanceRestorePolicy(
		context.Background(),
		reverseSpec,
		reverseInst,
		targetClient,
		WithBaselineClusters("cluster-a", "cluster-b"),
		WithApplyTarget(disasterv1.RestoreModifierApplyResourceSync),
	)
	if err != nil {
		t.Fatalf("expected reverse direction mapping to pass, got %v", err)
	}
	pvcRule := firstRuleByGroupResource(reverseSpec.ResourceModifierRules, "persistentvolumeclaims")
	if pvcRule == nil || len(pvcRule.Patches) != 1 {
		t.Fatalf("expected reverse pvc rule")
	}
	if pvcRule.Patches[0].Value != "sc-main" {
		t.Fatalf("expected reverse target class sc-main, got %s", pvcRule.Patches[0].Value)
	}

	forwardInst := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces:    []string{"app-ns"},
			RestorePolicy: basePolicy.DeepCopy(),
		},
		Status: disasterv1.DisasterInstanceStatus{
			PrimaryCluster:   "cluster-a",
			SecondaryCluster: "cluster-b",
		},
	}
	forwardSpec := &disasterv1.AppRestoreSpec{Template: velerov1.RestoreSpec{}}
	_, err = ApplyInstanceRestorePolicy(
		context.Background(),
		forwardSpec,
		forwardInst,
		targetClient,
		WithBaselineClusters("cluster-a", "cluster-b"),
		WithApplyTarget(disasterv1.RestoreModifierApplyResourceSync),
	)
	if err == nil {
		t.Fatalf("expected forward direction strict validation failure without legacy fallback")
	}
	if !strings.Contains(err.Error(), "StorageClassTargetNotFound") {
		t.Fatalf("expected StorageClassTargetNotFound, got %v", err)
	}
}

func TestApplyInstanceRestorePolicy_GateDisabledLegacyOnlyStillWorks(t *testing.T) {
	tests := []struct {
		name             string
		primary          string
		secondary        string
		wantRuleMinCount int
	}{
		{
			name:             "runtime status complete",
			primary:          "cluster-a",
			secondary:        "cluster-b",
			wantRuleMinCount: 2,
		},
		{
			name:             "runtime status incomplete",
			primary:          "cluster-a",
			secondary:        "",
			wantRuleMinCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance := &disasterv1.DisasterInstance{
				Spec: disasterv1.DisasterInstanceSpec{
					Namespaces: []string{"app-ns"},
					RestorePolicy: &disasterv1.RestorePolicy{
						UseUnifiedDirectionResolver: boolPtr(false),
						StorageClassMapping: &disasterv1.RestoreClassMappingPolicy{
							Mappings: []disasterv1.RestoreClassMapping{{
								SourceClass: "source-sc",
								TargetClass: "target-sc",
							}},
						},
					},
				},
				Status: disasterv1.DisasterInstanceStatus{
					PrimaryCluster:   tt.primary,
					SecondaryCluster: tt.secondary,
				},
			}

			spec := &disasterv1.AppRestoreSpec{Template: velerov1.RestoreSpec{}}
			_, err := ApplyInstanceRestorePolicy(
				context.Background(),
				spec,
				instance,
				nil,
				WithBaselineClusters("cluster-a", "cluster-b"),
				WithApplyTarget(disasterv1.RestoreModifierApplyResourceSync),
			)
			if err != nil {
				if strings.Contains(err.Error(), ModifierErrorFeatureDisabled) {
					t.Fatalf("legacy-only path should not return %s, got %v", ModifierErrorFeatureDisabled, err)
				}
				if strings.Contains(err.Error(), ModifierErrorDirectionResolveFailed) {
					t.Fatalf("legacy-only path should not return %s, got %v", ModifierErrorDirectionResolveFailed, err)
				}
				t.Fatalf("unexpected apply error: %v", err)
			}
			if got := len(spec.ResourceModifierRules); got < tt.wantRuleMinCount {
				t.Fatalf("expected at least %d legacy rules, got %d", tt.wantRuleMinCount, got)
			}
		})
	}
}

func TestApplyInstanceRestorePolicy_GateEnabledDSLAppliesAcrossPaths(t *testing.T) {
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app-ns"},
			RestorePolicy: &disasterv1.RestorePolicy{
				UseUnifiedDirectionResolver: boolPtr(true),
				ModifierRules: []disasterv1.RestoreModifierRule{{
					ID:       "all-paths",
					Mode:     disasterv1.RestoreModifierModeVeleroNative,
					ApplyTo:  []disasterv1.RestoreModifierApplyTarget{disasterv1.RestoreModifierApplyDataSync, disasterv1.RestoreModifierApplyResourceSync, disasterv1.RestoreModifierApplyDrill},
					Priority: 200,
					Conditions: disasterv1.Conditions{
						GroupResource: "pods",
					},
					VeleroRule: &disasterv1.RestoreModifierVeleroRule{
						Patches: []disasterv1.JSONPatch{{
							Operation: "add",
							Path:      "/metadata/annotations/patched-by",
							Value:     "platform",
						}},
					},
				}},
			},
		},
	}

	tests := []struct {
		name   string
		target disasterv1.RestoreModifierApplyTarget
		cfg    BuilderConfig
	}{
		{
			name:   "datasync",
			target: disasterv1.RestoreModifierApplyDataSync,
			cfg: BuilderConfig{
				RestoreType:        RestoreTypeData,
				BackupSource:       "ds-demo",
				BackupName:         "backup-001",
				TargetCluster:      "cluster-b",
				SourceCluster:      "cluster-a",
				StorageRepository:  "repo-main",
				IncludedNamespaces: []string{"app-ns"},
				DataResourceModifierRules: []disasterv1.ResourceModifierRule{{
					Conditions: disasterv1.Conditions{GroupResource: "pods"},
					Patches: []disasterv1.JSONPatch{{
						Operation: "add",
						Path:      "/metadata/labels",
						Value:     `{"trafficless":"true"}`,
					}},
				}},
			},
		},
		{
			name:   "resourcesync",
			target: disasterv1.RestoreModifierApplyResourceSync,
			cfg: BuilderConfig{
				RestoreType:        RestoreTypeResource,
				BackupSource:       "rs-demo",
				BackupName:         "backup-001",
				TargetCluster:      "cluster-b",
				SourceCluster:      "cluster-a",
				StorageRepository:  "repo-main",
				IncludedNamespaces: []string{"app-ns"},
			},
		},
		{
			name:   "drill",
			target: disasterv1.RestoreModifierApplyDrill,
			cfg: BuilderConfig{
				RestoreType:        RestoreTypeResource,
				BackupSource:       "drill-rs-demo",
				BackupName:         "backup-001",
				TargetCluster:      "cluster-b",
				SourceCluster:      "cluster-a",
				StorageRepository:  "repo-main",
				IncludedNamespaces: []string{"app-ns"},
				IsForDrill:         true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := BuildAppRestoreSpec(tt.cfg)
			summary, err := ApplyInstanceRestorePolicy(
				context.Background(),
				&spec,
				instance,
				nil,
				WithBaselineClusters("cluster-a", "cluster-b"),
				WithApplyTarget(tt.target),
			)
			if err != nil {
				t.Fatalf("apply policy failed: %v", err)
			}
			if !hasPatchPath(spec.ResourceModifierRules, "/metadata/annotations/patched-by") {
				t.Fatalf("expected DSL patch in %s path", tt.name)
			}
			if summary.AppliedRuleCount == 0 {
				t.Fatalf("expected appliedRuleCount > 0")
			}
		})
	}
}

func TestApplyInstanceRestorePolicy_DSLProductConsistentAcrossDataSyncResourceSyncDrill(t *testing.T) {
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app-ns"},
			RestorePolicy: &disasterv1.RestorePolicy{
				UseUnifiedDirectionResolver: boolPtr(true),
				ModifierRules: []disasterv1.RestoreModifierRule{{
					ID:       "consistent",
					Mode:     disasterv1.RestoreModifierModeVeleroNative,
					ApplyTo:  []disasterv1.RestoreModifierApplyTarget{disasterv1.RestoreModifierApplyDataSync, disasterv1.RestoreModifierApplyResourceSync, disasterv1.RestoreModifierApplyDrill},
					Priority: 300,
					Conditions: disasterv1.Conditions{
						GroupResource: "pods",
					},
					VeleroRule: &disasterv1.RestoreModifierVeleroRule{
						Patches: []disasterv1.JSONPatch{{
							Operation: "add",
							Path:      "/metadata/annotations/patched-by",
							Value:     "platform",
						}},
					},
				}},
			},
		},
	}

	cases := []struct {
		name   string
		target disasterv1.RestoreModifierApplyTarget
		cfg    BuilderConfig
	}{
		{
			name:   "datasync",
			target: disasterv1.RestoreModifierApplyDataSync,
			cfg: BuilderConfig{
				RestoreType:        RestoreTypeData,
				BackupSource:       "ds-demo",
				BackupName:         "backup-001",
				TargetCluster:      "cluster-b",
				SourceCluster:      "cluster-a",
				StorageRepository:  "repo-main",
				IncludedNamespaces: []string{"app-ns"},
				DataResourceModifierRules: []disasterv1.ResourceModifierRule{{
					Conditions: disasterv1.Conditions{GroupResource: "pods"},
					Patches: []disasterv1.JSONPatch{{
						Operation: "add",
						Path:      "/metadata/labels",
						Value:     `{"trafficless":"true"}`,
					}},
				}},
			},
		},
		{
			name:   "resourcesync",
			target: disasterv1.RestoreModifierApplyResourceSync,
			cfg: BuilderConfig{
				RestoreType:        RestoreTypeResource,
				BackupSource:       "rs-demo",
				BackupName:         "backup-001",
				TargetCluster:      "cluster-b",
				SourceCluster:      "cluster-a",
				StorageRepository:  "repo-main",
				IncludedNamespaces: []string{"app-ns"},
			},
		},
		{
			name:   "drill",
			target: disasterv1.RestoreModifierApplyDrill,
			cfg: BuilderConfig{
				RestoreType:        RestoreTypeResource,
				BackupSource:       "drill-rs-demo",
				BackupName:         "backup-001",
				TargetCluster:      "cluster-b",
				SourceCluster:      "cluster-a",
				StorageRepository:  "repo-main",
				IncludedNamespaces: []string{"app-ns"},
				IsForDrill:         true,
			},
		},
	}

	var expectedPath, expectedValue string
	for _, tt := range cases {
		spec := BuildAppRestoreSpec(tt.cfg)
		_, err := ApplyInstanceRestorePolicy(
			context.Background(),
			&spec,
			instance,
			nil,
			WithBaselineClusters("cluster-a", "cluster-b"),
			WithApplyTarget(tt.target),
		)
		if err != nil {
			t.Fatalf("%s apply policy failed: %v", tt.name, err)
		}
		patch, found := findPatchByPath(spec.ResourceModifierRules, "/metadata/annotations/patched-by")
		if !found {
			t.Fatalf("%s missing compiled DSL patch", tt.name)
		}
		if expectedPath == "" {
			expectedPath = patch.Path
			expectedValue = patch.Value
			continue
		}
		if patch.Path != expectedPath || patch.Value != expectedValue {
			t.Fatalf("%s patch mismatch: got path=%s value=%s want path=%s value=%s", tt.name, patch.Path, patch.Value, expectedPath, expectedValue)
		}
	}
}

func TestApplyInstanceRestorePolicy_PathConsistencyAcrossDataSyncResourceSyncDrill(t *testing.T) {
	includeClusterResources := true
	restorePVs := false
	itemTimeout := metav1.Duration{Duration: 7 * time.Minute}

	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app-ns"},
			RestorePolicy: &disasterv1.RestorePolicy{
				ResourceSelection: &disasterv1.RestoreResourceSelectionPolicy{
					IncludeClusterResources: &includeClusterResources,
				},
				Execution: &disasterv1.RestoreExecutionPolicy{
					ExistingResourcePolicy: "update",
					RestorePVs:             &restorePVs,
					ItemOperationTimeout:   &itemTimeout,
				},
				StorageClassMapping: &disasterv1.RestoreClassMappingPolicy{
					Mappings: []disasterv1.RestoreClassMapping{{
						SourceClass: "source-sc",
						TargetClass: "target-sc",
					}},
				},
				IngressClassMapping: &disasterv1.RestoreClassMappingPolicy{
					Mappings: []disasterv1.RestoreClassMapping{{
						SourceClass: "source-ing",
						TargetClass: "target-ing",
					}},
				},
			},
		},
	}

	baseRuleDataSync := []disasterv1.ResourceModifierRule{{
		Conditions: disasterv1.Conditions{GroupResource: "pods"},
		Patches: []disasterv1.JSONPatch{{
			Operation: "add",
			Path:      "/metadata/labels",
			Value:     `{"trafficless":"true"}`,
		}},
	}}
	baseRuleResourceSync := []disasterv1.ResourceModifierRule{{
		Conditions: disasterv1.Conditions{GroupResource: "deployments.apps"},
		Patches: []disasterv1.JSONPatch{{
			Operation: "replace",
			Path:      "/spec/template/spec/containers/0/image",
			Value:     "registry.target/app:v1",
		}},
	}}

	tests := []struct {
		name             string
		cfg              BuilderConfig
		expectedBaseRule int
	}{
		{
			name: "datasync",
			cfg: BuilderConfig{
				RestoreType:                RestoreTypeData,
				BackupSource:               "ds-demo",
				BackupName:                 "backup-001",
				TargetCluster:              "cluster-b",
				SourceCluster:              "cluster-a",
				StorageRepository:          "repo-main",
				IncludedNamespaces:         []string{"app-ns"},
				DataResourceModifierRules:  baseRuleDataSync,
				ExtraResourceModifierRules: nil,
			},
			expectedBaseRule: len(baseRuleDataSync),
		},
		{
			name: "resourcesync",
			cfg: BuilderConfig{
				RestoreType:                RestoreTypeResource,
				BackupSource:               "rs-demo",
				BackupName:                 "backup-001",
				TargetCluster:              "cluster-b",
				SourceCluster:              "cluster-a",
				StorageRepository:          "repo-main",
				IncludedNamespaces:         []string{"app-ns"},
				ExtraResourceModifierRules: baseRuleResourceSync,
			},
			expectedBaseRule: 2 + len(baseRuleResourceSync), // skeleton(2) + extra image rewrite
		},
		{
			name: "drill-resource",
			cfg: BuilderConfig{
				RestoreType:        RestoreTypeResource,
				BackupSource:       "drill-rs-demo",
				BackupName:         "backup-001",
				TargetCluster:      "cluster-b",
				SourceCluster:      "cluster-a",
				StorageRepository:  "repo-main",
				IncludedNamespaces: []string{"app-ns"},
				IsForDrill:         true,
				NamespaceMapping: map[string]string{
					"app-ns": "drill-ns",
				},
			},
			expectedBaseRule: 2, // skeleton rules only
		},
		{
			name: "drill-data",
			cfg: BuilderConfig{
				RestoreType:               RestoreTypeData,
				BackupSource:              "drill-ds-demo",
				BackupName:                "backup-001",
				TargetCluster:             "cluster-b",
				SourceCluster:             "cluster-a",
				StorageRepository:         "repo-main",
				IncludedNamespaces:        []string{"app-ns"},
				IsForDrill:                true,
				DataResourceModifierRules: baseRuleDataSync,
				NamespaceMapping: map[string]string{
					"app-ns": "drill-ns",
				},
			},
			expectedBaseRule: len(baseRuleDataSync),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := BuildAppRestoreSpec(tt.cfg)
			summary, err := ApplyInstanceRestorePolicy(context.Background(), &spec, instance, nil)
			if err != nil {
				t.Fatalf("apply policy failed: %v", err)
			}

			if summary.Source != "instance" {
				t.Fatalf("expected source=instance, got %s", summary.Source)
			}
			if spec.Template.ExistingResourcePolicy != velerov1.PolicyTypeUpdate {
				t.Fatalf("expected existingResourcePolicy=update, got %s", spec.Template.ExistingResourcePolicy)
			}
			if spec.Template.RestorePVs == nil || *spec.Template.RestorePVs != restorePVs {
				t.Fatalf("expected restorePVs=%v", restorePVs)
			}
			if spec.Template.ItemOperationTimeout != itemTimeout {
				t.Fatalf("expected itemOperationTimeout=%s, got %s", itemTimeout.Duration.String(), spec.Template.ItemOperationTimeout.Duration.String())
			}
			if spec.Template.IncludeClusterResources == nil || *spec.Template.IncludeClusterResources != includeClusterResources {
				t.Fatalf("expected includeClusterResources=%v", includeClusterResources)
			}
			if got := len(spec.ResourceModifierRules); got != tt.expectedBaseRule+3 {
				t.Fatalf("expected %d modifier rules, got %d", tt.expectedBaseRule+3, got)
			}
			if got := countRulesByGroupResource(spec.ResourceModifierRules, "persistentvolumeclaims"); got != 1 {
				t.Fatalf("expected 1 pvc mapping rule, got %d", got)
			}
			if got := countRulesByGroupResource(spec.ResourceModifierRules, "persistentvolumes"); got != 1 {
				t.Fatalf("expected 1 pv mapping rule, got %d", got)
			}
			if got := countRulesByGroupResource(spec.ResourceModifierRules, "ingresses.networking.k8s.io"); got != 1 {
				t.Fatalf("expected 1 ingress mapping rule, got %d", got)
			}
		})
	}
}

func TestBuildAndApplyRestorePolicy_CrossClusterClassMappingIntegration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := storagev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add storage scheme: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add networking scheme: %v", err)
	}

	targetClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "target-gold"}},
		&networkingv1.IngressClass{ObjectMeta: metav1.ObjectMeta{Name: "target-nginx"}},
	).Build()

	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"prod"},
			RestorePolicy: &disasterv1.RestorePolicy{
				Execution: &disasterv1.RestoreExecutionPolicy{
					ExistingResourcePolicy: "update",
				},
				StorageClassMapping: &disasterv1.RestoreClassMappingPolicy{
					StrictTargetValidation: true,
					UnmatchedPolicy:        disasterv1.RestoreClassUnmatchedPolicyKeep,
					Mappings: []disasterv1.RestoreClassMapping{{
						SourceClass: "source-standard",
						TargetClass: "target-gold",
						Namespaces:  []string{"prod"},
					}},
				},
				IngressClassMapping: &disasterv1.RestoreClassMappingPolicy{
					StrictTargetValidation: true,
					UnmatchedPolicy:        disasterv1.RestoreClassUnmatchedPolicyKeep,
					Mappings: []disasterv1.RestoreClassMapping{{
						SourceClass: "source-nginx",
						TargetClass: "target-nginx",
					}},
				},
			},
		},
	}

	spec := BuildAppRestoreSpec(BuilderConfig{
		RestoreType:        RestoreTypeResource,
		BackupSource:       "rs-prod",
		BackupName:         "backup-002",
		TargetCluster:      "cluster-dr",
		SourceCluster:      "cluster-main",
		StorageRepository:  "repo-main",
		IncludedNamespaces: []string{"prod"},
	})

	summary, err := ApplyInstanceRestorePolicy(context.Background(), &spec, instance, targetClient)
	if err != nil {
		t.Fatalf("expected mapping integration success, got error: %v", err)
	}
	if !summary.StorageStrictValidation || !summary.IngressStrictValidation {
		t.Fatalf("expected strict validation flags in summary")
	}
	if summary.StorageClassMappingCount != 1 || summary.IngressClassMappingCount != 1 {
		t.Fatalf("expected summary mapping counts = 1/1, got %d/%d", summary.StorageClassMappingCount, summary.IngressClassMappingCount)
	}

	pvcRule := firstRuleByGroupResource(spec.ResourceModifierRules, "persistentvolumeclaims")
	if pvcRule == nil {
		t.Fatalf("expected pvc mapping rule")
	}
	if len(pvcRule.Conditions.Namespaces) != 1 || pvcRule.Conditions.Namespaces[0] != "prod" {
		t.Fatalf("expected pvc mapping namespaces [prod], got %v", pvcRule.Conditions.Namespaces)
	}
	if len(pvcRule.Patches) != 1 || pvcRule.Patches[0].Value != "target-gold" {
		t.Fatalf("expected pvc storageClass patch target-gold, got %+v", pvcRule.Patches)
	}

	ingRule := firstRuleByGroupResource(spec.ResourceModifierRules, "ingresses.networking.k8s.io")
	if ingRule == nil {
		t.Fatalf("expected ingress mapping rule")
	}
	if len(ingRule.Patches) != 1 || ingRule.Patches[0].Value != "target-nginx" {
		t.Fatalf("expected ingressClass patch target-nginx, got %+v", ingRule.Patches)
	}

	meta := metav1.ObjectMeta{}
	ApplyPolicySummaryAnnotations(&meta, summary)
	if meta.Annotations[AnnotationRestorePolicySource] != "instance" {
		t.Fatalf("expected annotation restore-policy-source=instance, got %s", meta.Annotations[AnnotationRestorePolicySource])
	}
	if !strings.Contains(meta.Annotations[AnnotationRestorePolicySummary], `"storageClassMappingCount":1`) {
		t.Fatalf("expected summary annotation contains storageClassMappingCount")
	}
}

func countRulesByGroupResource(rules []disasterv1.ResourceModifierRule, groupResource string) int {
	count := 0
	for _, rule := range rules {
		if rule.Conditions.GroupResource == groupResource {
			count++
		}
	}
	return count
}

func firstRuleByGroupResource(rules []disasterv1.ResourceModifierRule, groupResource string) *disasterv1.ResourceModifierRule {
	for i := range rules {
		if rules[i].Conditions.GroupResource == groupResource {
			return &rules[i]
		}
	}
	return nil
}

func hasPatchPath(rules []disasterv1.ResourceModifierRule, path string) bool {
	for _, rule := range rules {
		for _, patch := range rule.Patches {
			if patch.Path == path {
				return true
			}
		}
	}
	return false
}

func findPatchByPath(rules []disasterv1.ResourceModifierRule, path string) (disasterv1.JSONPatch, bool) {
	for _, rule := range rules {
		for _, patch := range rule.Patches {
			if patch.Path == path {
				return patch, true
			}
		}
	}
	return disasterv1.JSONPatch{}, false
}

func TestApplyResourceSelectionPolicy_IncludeClusterResourcesTruePrefersOld(t *testing.T) {
	t.Parallel()

	includeClusterResources := true
	spec := &disasterv1.AppRestoreSpec{
		Template: velerov1.RestoreSpec{
			IncludedResources: []string{"default-old"},
			ExcludedResources: []string{"default-ex"},
		},
	}
	mode := applyResourceSelectionPolicy(spec, &disasterv1.RestoreResourceSelectionPolicy{
		IncludeClusterResources:          &includeClusterResources,
		IncludedResources:                []string{"deployments.apps"},
		ExcludedResources:                []string{"secrets"},
		IncludedNamespaceScopedResources: []string{"services"},
		IncludedClusterScopedResources:   []string{"nodes"},
		ExcludedClusterScopedResources:   []string{"*"},
	})

	if mode != resourceSelectionModeOld {
		t.Fatalf("expected mode=%s, got %s", resourceSelectionModeOld, mode)
	}
	if !equalStringSlices(spec.Template.IncludedResources, []string{"deployments.apps"}) {
		t.Fatalf("expected old included resources only, got %v", spec.Template.IncludedResources)
	}
	if !equalStringSlices(spec.Template.ExcludedResources, []string{"secrets"}) {
		t.Fatalf("expected old excluded resources only, got %v", spec.Template.ExcludedResources)
	}
	if spec.Template.IncludeClusterResources == nil || !*spec.Template.IncludeClusterResources {
		t.Fatalf("expected includeClusterResources=true, got %v", spec.Template.IncludeClusterResources)
	}
}

func TestApplyResourceSelectionPolicy_ScopedMapping(t *testing.T) {
	t.Parallel()

	spec := &disasterv1.AppRestoreSpec{
		Template: velerov1.RestoreSpec{
			IncludedResources: []string{"old-value-should-be-overwritten"},
			ExcludedResources: []string{"old-value-should-be-overwritten"},
		},
	}
	mode := applyResourceSelectionPolicy(spec, &disasterv1.RestoreResourceSelectionPolicy{
		IncludedResources:                []string{"ignored-old"},
		ExcludedResources:                []string{"ignored-old"},
		IncludedNamespaceScopedResources: []string{"deployments.apps", "services"},
		IncludedClusterScopedResources:   []string{"nodes", "services", "  "},
		ExcludedNamespaceScopedResources: []string{"secrets"},
		ExcludedClusterScopedResources:   []string{"clusterroles", "secrets"},
	})

	if mode != resourceSelectionModeScoped {
		t.Fatalf("expected mode=%s, got %s", resourceSelectionModeScoped, mode)
	}
	if !equalStringSlices(spec.Template.IncludedResources, []string{"deployments.apps", "services", "nodes"}) {
		t.Fatalf("unexpected included resources: %v", spec.Template.IncludedResources)
	}
	if !equalStringSlices(spec.Template.ExcludedResources, []string{"secrets", "clusterroles"}) {
		t.Fatalf("unexpected excluded resources: %v", spec.Template.ExcludedResources)
	}
	if spec.Template.IncludeClusterResources == nil || !*spec.Template.IncludeClusterResources {
		t.Fatalf("expected includeClusterResources=true in scoped mode")
	}
}

func TestApplyResourceSelectionPolicy_ScopedWildcardExcludeCluster(t *testing.T) {
	t.Parallel()

	preValue := true
	spec := &disasterv1.AppRestoreSpec{
		Template: velerov1.RestoreSpec{
			IncludeClusterResources: &preValue,
		},
	}
	mode := applyResourceSelectionPolicy(spec, &disasterv1.RestoreResourceSelectionPolicy{
		ExcludedClusterScopedResources: []string{"*"},
	})

	if mode != resourceSelectionModeScoped {
		t.Fatalf("expected mode=%s, got %s", resourceSelectionModeScoped, mode)
	}
	if spec.Template.IncludeClusterResources == nil || *spec.Template.IncludeClusterResources {
		t.Fatalf("expected includeClusterResources=false when excludedClusterScopedResources=[*], got %v", spec.Template.IncludeClusterResources)
	}
}

func TestApplyPolicySummaryAnnotations_WritesResourceSelectionMode(t *testing.T) {
	t.Parallel()

	meta := metav1.ObjectMeta{}
	ApplyPolicySummaryAnnotations(&meta, PolicySummary{
		Source:                "instance",
		ResourceSelectionMode: resourceSelectionModeScoped,
	})
	if got := meta.Annotations[AnnotationResourceSelectionMode]; got != resourceSelectionModeScoped {
		t.Fatalf("expected annotation %s=%s, got %s", AnnotationResourceSelectionMode, resourceSelectionModeScoped, got)
	}
}

func TestApplyInstanceRestorePolicy_SetsBulkModifierSummary(t *testing.T) {
	t.Parallel()

	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			RestorePolicy: &disasterv1.RestorePolicy{
				UseUnifiedDirectionResolver: boolPtr(true),
				BulkModifierActions: []disasterv1.BulkModifierAction{{
					ID:      "bulk-a",
					Action:  disasterv1.BulkModifierActionReplaceExactValue,
					Enabled: boolPtr(true),
				}},
				ModifierRuleSnapshot: []disasterv1.RestoreModifierRule{{
					ID:   "snapshot-rule",
					Mode: disasterv1.RestoreModifierModeVeleroNative,
					ApplyTo: []disasterv1.RestoreModifierApplyTarget{
						disasterv1.RestoreModifierApplyResourceSync,
					},
					Conditions: disasterv1.Conditions{
						GroupResource: "deployments.apps",
					},
					VeleroRule: &disasterv1.RestoreModifierVeleroRule{
						Patches: []disasterv1.JSONPatch{{
							Operation: "add",
							Path:      "/metadata/annotations/from-snapshot",
							Value:     "bulk",
						}},
					},
				}},
				ModifierRuleSnapshotHash: "sha256:bulk-hash",
			},
		},
	}
	spec := &disasterv1.AppRestoreSpec{
		Template: velerov1.RestoreSpec{},
	}

	summary, err := ApplyInstanceRestorePolicy(context.Background(), spec, instance, nil)
	if err != nil {
		t.Fatalf("expected bulk summary success, got %v", err)
	}
	if summary.ModifierSource != "bulkActions" {
		t.Fatalf("expected modifierSource=bulkActions, got %s", summary.ModifierSource)
	}
	if summary.ModifierBulkActionCount != 1 {
		t.Fatalf("expected modifierBulkActionCount=1, got %d", summary.ModifierBulkActionCount)
	}
	if summary.ModifierSnapshotHash != "sha256:bulk-hash" {
		t.Fatalf("expected snapshot hash, got %s", summary.ModifierSnapshotHash)
	}
	if len(spec.ResourceModifierRules) != 1 {
		t.Fatalf("expected compiled snapshot rules, got %d", len(spec.ResourceModifierRules))
	}

	meta := metav1.ObjectMeta{}
	ApplyPolicySummaryAnnotations(&meta, summary)
	if got := meta.Annotations[AnnotationModifierSource]; got != "bulkActions" {
		t.Fatalf("expected modifier-source annotation, got %s", got)
	}
	if got := meta.Annotations[AnnotationModifierBulkActionCount]; got != "1" {
		t.Fatalf("expected bulk action count annotation, got %s", got)
	}
	if got := meta.Annotations[AnnotationModifierSnapshotHash]; got != "sha256:bulk-hash" {
		t.Fatalf("expected snapshot hash annotation, got %s", got)
	}
}

func TestApplyInstanceRestorePolicy_BulkSnapshotIsFinalInputAcrossResourceSyncAndDrill(t *testing.T) {
	t.Parallel()

	makePolicy := func() *disasterv1.RestorePolicy {
		targets := []disasterv1.RestoreModifierApplyTarget{
			disasterv1.RestoreModifierApplyResourceSync,
			disasterv1.RestoreModifierApplyDrill,
		}
		return &disasterv1.RestorePolicy{
			UseUnifiedDirectionResolver: boolPtr(true),
			BulkModifierActions: []disasterv1.BulkModifierAction{{
				ID:      "bulk-action",
				Action:  disasterv1.BulkModifierActionReplaceExactValue,
				Enabled: boolPtr(true),
			}},
			ModifierRules: []disasterv1.RestoreModifierRule{{
				ID:      "manual-rule-stale-input",
				Mode:    disasterv1.RestoreModifierModeVeleroNative,
				ApplyTo: targets,
				Conditions: disasterv1.Conditions{
					GroupResource: "deployments.apps",
				},
				VeleroRule: &disasterv1.RestoreModifierVeleroRule{
					Patches: []disasterv1.JSONPatch{{
						Operation: "add",
						Path:      "/metadata/annotations/from-modifier-rules",
						Value:     "must-not-append",
					}},
				},
			}},
			ModifierRuleSnapshot: []disasterv1.RestoreModifierRule{
				{
					ID:      "bulk-generated-rule",
					Mode:    disasterv1.RestoreModifierModeVeleroNative,
					ApplyTo: targets,
					Conditions: disasterv1.Conditions{
						GroupResource: "deployments.apps",
					},
					VeleroRule: &disasterv1.RestoreModifierVeleroRule{
						Patches: []disasterv1.JSONPatch{{
							Operation: "add",
							Path:      "/metadata/annotations/from-bulk-snapshot",
							Value:     "bulk",
						}},
					},
				},
				{
					ID:      "manual-rule-in-snapshot",
					Mode:    disasterv1.RestoreModifierModeVeleroNative,
					ApplyTo: targets,
					Conditions: disasterv1.Conditions{
						GroupResource: "deployments.apps",
					},
					VeleroRule: &disasterv1.RestoreModifierVeleroRule{
						Patches: []disasterv1.JSONPatch{{
							Operation: "add",
							Path:      "/metadata/annotations/from-manual-snapshot",
							Value:     "manual",
						}},
					},
				},
			},
			ModifierRuleSnapshotHash: "sha256:snapshot-final-input",
		}
	}

	cases := []struct {
		name        string
		target      disasterv1.RestoreModifierApplyTarget
		useOverride bool
		wantSource  string
	}{
		{
			name:       "resourcesync",
			target:     disasterv1.RestoreModifierApplyResourceSync,
			wantSource: "instance",
		},
		{
			name:        "drill",
			target:      disasterv1.RestoreModifierApplyDrill,
			useOverride: true,
			wantSource:  "drillOverride",
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			instance := &disasterv1.DisasterInstance{
				Status: disasterv1.DisasterInstanceStatus{
					PrimaryCluster:   "cluster-a",
					SecondaryCluster: "cluster-b",
				},
				Spec: disasterv1.DisasterInstanceSpec{
					RestorePolicy: makePolicy(),
				},
			}
			opts := []ApplyInstanceRestorePolicyOption{
				WithBaselineClusters("cluster-a", "cluster-b"),
				WithApplyTarget(tt.target),
			}
			if tt.useOverride {
				opts = append(opts, WithRestorePolicyOverride(makePolicy()))
			}

			spec := &disasterv1.AppRestoreSpec{Template: velerov1.RestoreSpec{}}
			summary, err := ApplyInstanceRestorePolicy(context.Background(), spec, instance, nil, opts...)
			if err != nil {
				t.Fatalf("ApplyInstanceRestorePolicy returned error: %v", err)
			}
			if summary.Source != tt.wantSource {
				t.Fatalf("expected source=%s, got %s", tt.wantSource, summary.Source)
			}
			if summary.ModifierSource != "bulkActions" {
				t.Fatalf("expected modifierSource=bulkActions, got %s", summary.ModifierSource)
			}
			if summary.ModifierSnapshotHash != "sha256:snapshot-final-input" {
				t.Fatalf("expected snapshot hash annotation source, got %s", summary.ModifierSnapshotHash)
			}
			if got := len(spec.ResourceModifierRules); got != 2 {
				t.Fatalf("expected exactly snapshot rules without modifierRules append, got %d", got)
			}
			if !hasPatchPath(spec.ResourceModifierRules, "/metadata/annotations/from-bulk-snapshot") {
				t.Fatalf("expected bulk-generated snapshot rule in %s path", tt.name)
			}
			if !hasPatchPath(spec.ResourceModifierRules, "/metadata/annotations/from-manual-snapshot") {
				t.Fatalf("expected manual snapshot rule in %s path", tt.name)
			}
			if hasPatchPath(spec.ResourceModifierRules, "/metadata/annotations/from-modifier-rules") {
				t.Fatalf("modifierRules were appended outside snapshot in %s path", tt.name)
			}
		})
	}
}

func equalStringSlices(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
