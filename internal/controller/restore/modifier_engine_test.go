package restore

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

func TestResolveModifierFlow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		baselineSource   string
		baselineTarget   string
		runtimePrimary   string
		runtimeSecondary string
		wantFlow         modifierFlow
		wantSource       directionSource
		wantErrCode      string
	}{
		{
			name:             "fallback forward when runtime is empty",
			baselineSource:   "cluster-a",
			baselineTarget:   "cluster-b",
			runtimePrimary:   "",
			runtimeSecondary: "",
			wantFlow:         modifierFlowForward,
			wantSource:       directionSourceBaselineFallback,
		},
		{
			name:             "forward when runtime equals baseline",
			baselineSource:   "cluster-a",
			baselineTarget:   "cluster-b",
			runtimePrimary:   "cluster-a",
			runtimeSecondary: "cluster-b",
			wantFlow:         modifierFlowForward,
			wantSource:       directionSourceRuntimeStatus,
		},
		{
			name:             "reverse when runtime equals reversed baseline",
			baselineSource:   "cluster-a",
			baselineTarget:   "cluster-b",
			runtimePrimary:   "cluster-b",
			runtimeSecondary: "cluster-a",
			wantFlow:         modifierFlowReverse,
			wantSource:       directionSourceRuntimeStatus,
		},
		{
			name:             "fail when runtime only one side exists",
			baselineSource:   "cluster-a",
			baselineTarget:   "cluster-b",
			runtimePrimary:   "cluster-a",
			runtimeSecondary: "",
			wantErrCode:      ModifierErrorDirectionResolveFailed,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotFlow, gotSource, err := resolveModifierFlow(
				tt.baselineSource,
				tt.baselineTarget,
				tt.runtimePrimary,
				tt.runtimeSecondary,
			)
			if tt.wantErrCode != "" {
				if err == nil {
					t.Fatalf("expected error %s, got nil", tt.wantErrCode)
				}
				if !strings.Contains(err.Error(), tt.wantErrCode) {
					t.Fatalf("expected error contains %s, got %v", tt.wantErrCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotFlow != tt.wantFlow {
				t.Fatalf("unexpected flow: got %s want %s", gotFlow, tt.wantFlow)
			}
			if gotSource != tt.wantSource {
				t.Fatalf("unexpected source: got %s want %s", gotSource, tt.wantSource)
			}
		})
	}
}

func TestCompileModifierRulesForInstance_GateBehavior(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(false, []disasterv1.RestoreModifierRule{
		{
			ID:   "dsl-1",
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
		},
	})

	_, _, err := compileModifierRulesForInstance(instance, ApplyInstanceRestorePolicyOptions{})
	if err == nil {
		t.Fatalf("expected feature-disabled error, got nil")
	}
	if !strings.Contains(err.Error(), ModifierErrorFeatureDisabled) {
		t.Fatalf("expected %s, got %v", ModifierErrorFeatureDisabled, err)
	}
}

func TestCompileModifierRulesForInstance_UsesSnapshotWhenEnabledBulkActionsExist(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{{
		ID:   "manual-rule",
		Mode: disasterv1.RestoreModifierModeVeleroNative,
		Conditions: disasterv1.Conditions{
			GroupResource: "deployments.apps",
		},
		VeleroRule: &disasterv1.RestoreModifierVeleroRule{
			Patches: []disasterv1.JSONPatch{{
				Operation: "add",
				Path:      "/metadata/annotations/manual",
				Value:     "manual",
			}},
		},
	}})
	instance.Spec.RestorePolicy.BulkModifierActions = []disasterv1.BulkModifierAction{{
		ID:      "bulk-a",
		Action:  disasterv1.BulkModifierActionReplaceExactValue,
		Enabled: boolPtr(true),
	}}
	instance.Spec.RestorePolicy.ModifierRuleSnapshot = []disasterv1.RestoreModifierRule{{
		ID:   "snapshot-rule",
		Mode: disasterv1.RestoreModifierModeVeleroNative,
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
	}}
	instance.Spec.RestorePolicy.ModifierRuleSnapshotHash = "sha256:test"

	compiled, _, err := compileModifierRulesForInstance(instance, ApplyInstanceRestorePolicyOptions{})
	if err != nil {
		t.Fatalf("expected snapshot compile success, got %v", err)
	}
	if len(compiled) != 1 {
		t.Fatalf("expected one compiled rule from snapshot, got %d", len(compiled))
	}
	if got := compiled[0].Patches[0].Path; got != "/metadata/annotations/from-snapshot" {
		t.Fatalf("expected snapshot patch path, got %s", got)
	}
}

func TestCompileModifierRulesForInstance_AllBulkActionsDisabledFallsBackToModifierRules(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{{
		ID:   "manual-rule",
		Mode: disasterv1.RestoreModifierModeVeleroNative,
		Conditions: disasterv1.Conditions{
			GroupResource: "deployments.apps",
		},
		VeleroRule: &disasterv1.RestoreModifierVeleroRule{
			Patches: []disasterv1.JSONPatch{{
				Operation: "add",
				Path:      "/metadata/annotations/manual",
				Value:     "manual",
			}},
		},
	}})
	instance.Spec.RestorePolicy.BulkModifierActions = []disasterv1.BulkModifierAction{{
		ID:      "bulk-disabled",
		Action:  disasterv1.BulkModifierActionReplaceExactValue,
		Enabled: boolPtr(false),
	}}
	instance.Spec.RestorePolicy.ModifierRuleSnapshot = []disasterv1.RestoreModifierRule{{
		ID:   "snapshot-rule",
		Mode: disasterv1.RestoreModifierModeVeleroNative,
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
	}}
	instance.Spec.RestorePolicy.ModifierRuleSnapshotHash = "sha256:test"

	compiled, _, err := compileModifierRulesForInstance(instance, ApplyInstanceRestorePolicyOptions{})
	if err != nil {
		t.Fatalf("expected modifierRules fallback success, got %v", err)
	}
	if len(compiled) != 1 {
		t.Fatalf("expected one compiled manual rule, got %d", len(compiled))
	}
	if got := compiled[0].Patches[0].Path; got != "/metadata/annotations/manual" {
		t.Fatalf("expected manual patch path, got %s", got)
	}
}

func TestCompileModifierRulesForInstance_EnabledBulkActionMissingSnapshotRejected(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, nil)
	instance.Spec.RestorePolicy.BulkModifierActions = []disasterv1.BulkModifierAction{{
		ID:      "bulk-a",
		Action:  disasterv1.BulkModifierActionReplaceExactValue,
		Enabled: boolPtr(true),
	}}

	_, _, err := compileModifierRulesForInstance(instance, ApplyInstanceRestorePolicyOptions{})
	if err == nil {
		t.Fatalf("expected missing snapshot rejection")
	}
	if !strings.Contains(err.Error(), ModifierErrorRuleRejected) {
		t.Fatalf("expected %s, got %v", ModifierErrorRuleRejected, err)
	}
	if !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("expected snapshot detail, got %v", err)
	}
}

func TestCompileModifierRulesForInstance_RewriteImageRuntimeRulesDoNotRequireSnapshot(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(false, nil)
	instance.Spec.RestorePolicy.UseUnifiedDirectionResolver = boolPtr(true)
	instance.Spec.RestorePolicy.BulkModifierActions = []disasterv1.BulkModifierAction{{
		ID:      "rewrite-primary",
		Action:  disasterv1.BulkModifierActionRewriteImage,
		Enabled: boolPtr(true),
		ApplyTo: []disasterv1.RestoreModifierApplyTarget{
			disasterv1.RestoreModifierApplyResourceSync,
		},
		ImageRewrite: &disasterv1.DynamicImageRewriteConfig{
			SourcePrefix: "10.11.11.1:5000/",
			TargetPrefix: "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/",
		},
	}}
	instance.Spec.RestorePolicy.ModifierRuleSnapshot = nil
	instance.Spec.RestorePolicy.ModifierRuleSnapshotHash = ""

	compiled, summary, err := compileModifierRulesForInstance(
		instance,
		ApplyInstanceRestorePolicyOptions{
			BaselineSourceCluster: "cluster-a",
			BaselineTargetCluster: "cluster-b",
			ApplyTarget:           disasterv1.RestoreModifierApplyResourceSync,
			RuntimeModifierRules: []disasterv1.RestoreModifierRule{{
				ID:       "runtime-image-rewrite-0000-0000",
				Mode:     disasterv1.RestoreModifierModeReversible,
				ApplyTo:  []disasterv1.RestoreModifierApplyTarget{disasterv1.RestoreModifierApplyResourceSync},
				Priority: -100,
				Conditions: disasterv1.Conditions{
					GroupResource:     "deployments.apps",
					ResourceNameRegex: "^demo$",
					Namespaces:        []string{"demo"},
				},
				DirectionPolicy: disasterv1.RestoreModifierDirectionPolicyAuto,
				Pair: &disasterv1.RestoreModifierPair{
					Path:        "/spec/template/spec/containers/0/image",
					SourceValue: "10.11.11.1:5000/blueking/app:v1.31.0",
					TargetValue: "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/app:v1.31.0",
				},
			}},
		},
	)
	if err != nil {
		t.Fatalf("expected runtime rewriteImage compile success without snapshot, got %v", err)
	}
	if len(compiled) != 1 {
		t.Fatalf("expected one compiled runtime rule, got %d", len(compiled))
	}
	if got := compiled[0].Patches[0].Value; got != "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/app:v1.31.0" {
		t.Fatalf("unexpected compiled target value: %s", got)
	}
	if summary.AppliedRuleCount != 1 {
		t.Fatalf("expected applied rule count 1, got %+v", summary)
	}
}

func TestCompileModifierRulesForInstance_ComplexityLimits(t *testing.T) {
	t.Parallel()

	t.Run("rule count limit", func(t *testing.T) {
		t.Parallel()

		rules := make([]disasterv1.RestoreModifierRule, 0, maxModifierRulesPerInstance+1)
		for i := 0; i < maxModifierRulesPerInstance+1; i++ {
			rules = append(rules, disasterv1.RestoreModifierRule{
				ID:   fmt.Sprintf("rule-%03d", i),
				Mode: disasterv1.RestoreModifierModeVeleroNative,
				Conditions: disasterv1.Conditions{
					GroupResource: "deployments.apps",
				},
				VeleroRule: &disasterv1.RestoreModifierVeleroRule{
					Patches: []disasterv1.JSONPatch{{
						Operation: "add",
						Path:      "/metadata/annotations/" + strconv.Itoa(i),
						Value:     "x",
					}},
				},
			})
		}
		instance := baseInstanceWithPolicy(true, rules)
		_, _, err := compileModifierRulesForInstance(instance, ApplyInstanceRestorePolicyOptions{})
		if err == nil {
			t.Fatalf("expected rule count rejection")
		}
		if !strings.Contains(err.Error(), ModifierErrorRuleRejected) {
			t.Fatalf("expected %s, got %v", ModifierErrorRuleRejected, err)
		}
		if !strings.Contains(err.Error(), "exceeds limit") {
			t.Fatalf("expected limit detail, got %v", err)
		}
	})

	t.Run("path depth limit", func(t *testing.T) {
		t.Parallel()

		segments := make([]string, 0, maxJSONPointerDepth+1)
		for i := 0; i < maxJSONPointerDepth+1; i++ {
			segments = append(segments, "k")
		}
		deepPath := "/" + strings.Join(segments, "/")

		instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{
			{
				ID:   "deep-path",
				Mode: disasterv1.RestoreModifierModeVeleroNative,
				Conditions: disasterv1.Conditions{
					GroupResource: "deployments.apps",
				},
				VeleroRule: &disasterv1.RestoreModifierVeleroRule{
					Patches: []disasterv1.JSONPatch{{
						Operation: "add",
						Path:      deepPath,
						Value:     "x",
					}},
				},
			},
		})
		_, _, err := compileModifierRulesForInstance(instance, ApplyInstanceRestorePolicyOptions{})
		if err == nil {
			t.Fatalf("expected deep path rejection")
		}
		if !strings.Contains(err.Error(), ModifierErrorRuleRejected) {
			t.Fatalf("expected %s, got %v", ModifierErrorRuleRejected, err)
		}
		if !strings.Contains(err.Error(), "depth") {
			t.Fatalf("expected depth detail, got %v", err)
		}
	})

	t.Run("resourceNameRegex complexity limit", func(t *testing.T) {
		t.Parallel()

		longRegex := strings.Repeat("a", maxResourceNameRegexLength+1)
		instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{
			{
				ID:   "long-regex",
				Mode: disasterv1.RestoreModifierModeVeleroNative,
				Conditions: disasterv1.Conditions{
					GroupResource:     "deployments.apps",
					ResourceNameRegex: longRegex,
				},
				VeleroRule: &disasterv1.RestoreModifierVeleroRule{
					Patches: []disasterv1.JSONPatch{{
						Operation: "add",
						Path:      "/metadata/annotations/patched-by",
						Value:     "platform",
					}},
				},
			},
		})
		_, _, err := compileModifierRulesForInstance(instance, ApplyInstanceRestorePolicyOptions{})
		if err == nil {
			t.Fatalf("expected regex complexity rejection")
		}
		if !strings.Contains(err.Error(), ModifierErrorRuleRejected) {
			t.Fatalf("expected %s, got %v", ModifierErrorRuleRejected, err)
		}
		if !strings.Contains(err.Error(), "resourceNameRegex") {
			t.Fatalf("expected regex detail, got %v", err)
		}
	})
}

func TestCompileModifierRulesForInstance_TrafficlessOwnerReferencesGovernance(t *testing.T) {
	t.Parallel()

	t.Run("system trafficless remove ownerReferences is allowed", func(t *testing.T) {
		t.Parallel()

		compiled, _, err := compileModifierRulesForInstance(
			nil,
			ApplyInstanceRestorePolicyOptions{
				SystemRules: []disasterv1.ResourceModifierRule{{
					Conditions: disasterv1.Conditions{GroupResource: "pods"},
					Patches: []disasterv1.JSONPatch{{
						Operation: "remove",
						Path:      "/metadata/ownerReferences",
					}},
				}},
			},
		)
		if err != nil {
			t.Fatalf("expected system trafficless ownerReferences remove to pass, got %v", err)
		}
		if len(compiled) != 1 || len(compiled[0].Patches) != 1 {
			t.Fatalf("expected one compiled system rule, got %+v", compiled)
		}
		if got := compiled[0].Patches[0].Path; got != "/metadata/ownerReferences" {
			t.Fatalf("unexpected path: %s", got)
		}
	})

	t.Run("system trafficless add empty ownerReferences is allowed", func(t *testing.T) {
		t.Parallel()

		compiled, _, err := compileModifierRulesForInstance(
			nil,
			ApplyInstanceRestorePolicyOptions{
				SystemRules: []disasterv1.ResourceModifierRule{{
					Conditions: disasterv1.Conditions{GroupResource: "pods"},
					Patches: []disasterv1.JSONPatch{{
						Operation: "add",
						Path:      "/metadata/ownerReferences",
						Value:     "[]",
					}},
				}},
			},
		)
		if err != nil {
			t.Fatalf("expected system trafficless ownerReferences add-empty to pass, got %v", err)
		}
		if len(compiled) != 1 || len(compiled[0].Patches) != 1 {
			t.Fatalf("expected one compiled system rule, got %+v", compiled)
		}
		if got := compiled[0].Patches[0].Value; got != "[]" {
			t.Fatalf("unexpected ownerReferences value: %s", got)
		}
	})

	t.Run("user rule touching ownerReferences is rejected", func(t *testing.T) {
		t.Parallel()

		instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{
			{
				ID:   "user-owner-ref",
				Mode: disasterv1.RestoreModifierModeVeleroNative,
				Conditions: disasterv1.Conditions{
					GroupResource: "pods",
				},
				VeleroRule: &disasterv1.RestoreModifierVeleroRule{
					Patches: []disasterv1.JSONPatch{{
						Operation: "remove",
						Path:      "/metadata/ownerReferences",
					}},
				},
			},
		})

		_, _, err := compileModifierRulesForInstance(instance, ApplyInstanceRestorePolicyOptions{})
		if err == nil {
			t.Fatalf("expected ownerReferences governance rejection")
		}
		if !strings.Contains(err.Error(), ModifierErrorRuleRejected) {
			t.Fatalf("expected %s, got %v", ModifierErrorRuleRejected, err)
		}
	})
}

func TestCompileModifierRulesForInstance_GovernanceForbiddenPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
	}{
		{name: "status root", path: "/status"},
		{name: "status sub field", path: "/status/phase"},
		{name: "metadata finalizers", path: "/metadata/finalizers"},
		{name: "metadata finalizers item", path: "/metadata/finalizers/0"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{{
				ID:   "forbidden-path",
				Mode: disasterv1.RestoreModifierModeVeleroNative,
				Conditions: disasterv1.Conditions{
					GroupResource: "deployments.apps",
				},
				VeleroRule: &disasterv1.RestoreModifierVeleroRule{
					Patches: []disasterv1.JSONPatch{{
						Operation: "add",
						Path:      tc.path,
						Value:     "x",
					}},
				},
			}})

			_, _, err := compileModifierRulesForInstance(instance, ApplyInstanceRestorePolicyOptions{})
			if err == nil {
				t.Fatalf("expected governance rejection for path %s", tc.path)
			}
			if !strings.Contains(err.Error(), ModifierErrorRuleRejected) {
				t.Fatalf("expected %s, got %v", ModifierErrorRuleRejected, err)
			}
		})
	}
}

func TestCompileModifierRulesForInstance_VeleroNativeMissingRuleRejected(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{{
		ID:   "missing-velero-rule",
		Mode: disasterv1.RestoreModifierModeVeleroNative,
		Conditions: disasterv1.Conditions{
			GroupResource: "deployments.apps",
		},
	}})

	_, _, err := compileModifierRulesForInstance(instance, ApplyInstanceRestorePolicyOptions{})
	if err == nil {
		t.Fatalf("expected missing veleroRule rejection")
	}
	if !strings.Contains(err.Error(), ModifierErrorRuleRejected) {
		t.Fatalf("expected %s, got %v", ModifierErrorRuleRejected, err)
	}
	if !strings.Contains(err.Error(), "missing veleroRule") {
		t.Fatalf("expected missing veleroRule detail, got %v", err)
	}
}

func TestCompileModifierRulesForInstance_ReversiblePairForwardReverse(t *testing.T) {
	t.Parallel()

	rules := []disasterv1.RestoreModifierRule{
		{
			ID:       "svc-nodeport",
			Mode:     disasterv1.RestoreModifierModeReversible,
			ApplyTo:  []disasterv1.RestoreModifierApplyTarget{disasterv1.RestoreModifierApplyDataSync},
			Priority: 200,
			Conditions: disasterv1.Conditions{
				GroupResource: "services",
			},
			Pair: &disasterv1.RestoreModifierPair{
				Path:        "/spec/ports/0/nodePort",
				SourceValue: "30080",
				TargetValue: "32080",
			},
		},
	}

	forwardInst := baseInstanceWithPolicy(true, rules)
	forwardInst.Status.PrimaryCluster = "cluster-a"
	forwardInst.Status.SecondaryCluster = "cluster-b"
	compiled, summary, err := compileModifierRulesForInstance(
		forwardInst,
		ApplyInstanceRestorePolicyOptions{
			BaselineSourceCluster: "cluster-a",
			BaselineTargetCluster: "cluster-b",
			ApplyTarget:           disasterv1.RestoreModifierApplyDataSync,
		},
	)
	if err != nil {
		t.Fatalf("unexpected forward compile error: %v", err)
	}
	if summary.Flow != string(modifierFlowForward) {
		t.Fatalf("unexpected flow summary: %s", summary.Flow)
	}
	if len(compiled) != 1 {
		t.Fatalf("expected 1 compiled rule, got %d", len(compiled))
	}
	if got := compiled[0].Patches[0].Value; got != "32080" {
		t.Fatalf("unexpected forward patch value: %s", got)
	}

	reverseInst := baseInstanceWithPolicy(true, rules)
	reverseInst.Status.PrimaryCluster = "cluster-b"
	reverseInst.Status.SecondaryCluster = "cluster-a"
	compiled, summary, err = compileModifierRulesForInstance(
		reverseInst,
		ApplyInstanceRestorePolicyOptions{
			BaselineSourceCluster: "cluster-a",
			BaselineTargetCluster: "cluster-b",
			ApplyTarget:           disasterv1.RestoreModifierApplyDataSync,
		},
	)
	if err != nil {
		t.Fatalf("unexpected reverse compile error: %v", err)
	}
	if summary.Flow != string(modifierFlowReverse) {
		t.Fatalf("unexpected reverse flow summary: %s", summary.Flow)
	}
	if got := compiled[0].Patches[0].Value; got != "30080" {
		t.Fatalf("unexpected reverse patch value: %s", got)
	}
}

func TestCompileModifierRulesForInstance_ReversiblePairMetadataStringValuePreserved(t *testing.T) {
	t.Parallel()

	rules := []disasterv1.RestoreModifierRule{
		{
			ID:   "site-role",
			Mode: disasterv1.RestoreModifierModeReversible,
			Conditions: disasterv1.Conditions{
				GroupResource: "deployments.apps",
			},
			Pair: &disasterv1.RestoreModifierPair{
				Path:        "/metadata/annotations/testudo.softcdata.com~1site-role",
				SourceValue: "1",
				TargetValue: "2",
			},
		},
	}

	instance := baseInstanceWithPolicy(true, rules)
	instance.Status.PrimaryCluster = "cluster-a"
	instance.Status.SecondaryCluster = "cluster-b"

	compiled, _, err := compileModifierRulesForInstance(
		instance,
		ApplyInstanceRestorePolicyOptions{
			BaselineSourceCluster: "cluster-a",
			BaselineTargetCluster: "cluster-b",
		},
	)
	if err != nil {
		t.Fatalf("unexpected forward compile error: %v", err)
	}
	if got := compiled[0].Patches[0].Value; got != strconv.Quote("2") {
		t.Fatalf("unexpected forward annotation value: got %q want %q", got, strconv.Quote("2"))
	}

	instance.Status.PrimaryCluster = "cluster-b"
	instance.Status.SecondaryCluster = "cluster-a"
	compiled, _, err = compileModifierRulesForInstance(
		instance,
		ApplyInstanceRestorePolicyOptions{
			BaselineSourceCluster: "cluster-a",
			BaselineTargetCluster: "cluster-b",
		},
	)
	if err != nil {
		t.Fatalf("unexpected reverse compile error: %v", err)
	}
	if got := compiled[0].Patches[0].Value; got != strconv.Quote("1") {
		t.Fatalf("unexpected reverse annotation value: got %q want %q", got, strconv.Quote("1"))
	}
}

func TestCompileModifierRulesForInstance_ReversibleAppendPathRejected(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{
		{
			ID:   "bad-append",
			Mode: disasterv1.RestoreModifierModeReversible,
			Conditions: disasterv1.Conditions{
				GroupResource: "deployments.apps",
			},
			Pair: &disasterv1.RestoreModifierPair{
				Path:        "/spec/template/spec/containers/-",
				SourceValue: "y",
				TargetValue: "x",
			},
		},
	})
	instance.Status.PrimaryCluster = "cluster-a"
	instance.Status.SecondaryCluster = "cluster-b"

	_, _, err := compileModifierRulesForInstance(
		instance,
		ApplyInstanceRestorePolicyOptions{
			BaselineSourceCluster: "cluster-a",
			BaselineTargetCluster: "cluster-b",
		},
	)
	if err == nil {
		t.Fatalf("expected append-path rejection, got nil")
	}
	if !strings.Contains(err.Error(), ModifierErrorRuleNotReversible) {
		t.Fatalf("expected %s, got %v", ModifierErrorRuleNotReversible, err)
	}
}

func TestCompileModifierRulesForInstance_ConflictHandling(t *testing.T) {
	t.Parallel()

	baseRules := []disasterv1.RestoreModifierRule{
		{
			ID:       "rule-a",
			Mode:     disasterv1.RestoreModifierModeVeleroNative,
			Priority: 100,
			Conditions: disasterv1.Conditions{
				GroupResource: "deployments.apps",
			},
			VeleroRule: &disasterv1.RestoreModifierVeleroRule{
				Patches: []disasterv1.JSONPatch{{
					Operation: "add",
					Path:      "/metadata/annotations/patched-by",
					Value:     "A",
				}},
			},
		},
		{
			ID:       "rule-b",
			Mode:     disasterv1.RestoreModifierModeVeleroNative,
			Priority: 100,
			Conditions: disasterv1.Conditions{
				GroupResource: "deployments.apps",
			},
			VeleroRule: &disasterv1.RestoreModifierVeleroRule{
				Patches: []disasterv1.JSONPatch{{
					Operation: "add",
					Path:      "/metadata/annotations/patched-by",
					Value:     "B",
				}},
			},
		},
	}

	instance := baseInstanceWithPolicy(true, baseRules)
	_, _, err := compileModifierRulesForInstance(instance, ApplyInstanceRestorePolicyOptions{})
	if err == nil {
		t.Fatalf("expected conflict error, got nil")
	}
	if !strings.Contains(err.Error(), ModifierErrorRuleConflict) {
		t.Fatalf("expected conflict error %s, got %v", ModifierErrorRuleConflict, err)
	}

	skipRules := append([]disasterv1.RestoreModifierRule{}, baseRules...)
	skipRules[1].OnConflict = disasterv1.RestoreModifierConflictPolicySkip
	instance = baseInstanceWithPolicy(true, skipRules)
	compiled, summary, err := compileModifierRulesForInstance(instance, ApplyInstanceRestorePolicyOptions{})
	if err != nil {
		t.Fatalf("unexpected skip-on-conflict error: %v", err)
	}
	if len(compiled) != 1 {
		t.Fatalf("expected 1 compiled rule after skip conflict, got %d", len(compiled))
	}
	if summary.SkippedRuleCount == 0 {
		t.Fatalf("expected skippedRuleCount > 0")
	}
}

func TestCompileModifierRulesForInstance_SystemProtectOverride(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{
		{
			ID:       "user-high-priority",
			Mode:     disasterv1.RestoreModifierModeVeleroNative,
			Priority: 500,
			Conditions: disasterv1.Conditions{
				GroupResource: "deployments.apps",
			},
			VeleroRule: &disasterv1.RestoreModifierVeleroRule{
				Patches: []disasterv1.JSONPatch{{
					Operation: "add",
					Path:      "/metadata/annotations/patched-by",
					Value:     "user",
				}},
			},
		},
	})

	compiled, summary, err := compileModifierRulesForInstance(
		instance,
		ApplyInstanceRestorePolicyOptions{
			SystemProtectRules: []disasterv1.ResourceModifierRule{{
				Conditions: disasterv1.Conditions{
					GroupResource: "deployments.apps",
				},
				Patches: []disasterv1.JSONPatch{{
					Operation: "add",
					Path:      "/metadata/annotations/patched-by",
					Value:     "system-protect",
				}},
			}},
		},
	)
	if err != nil {
		t.Fatalf("expected system-protect override success, got %v", err)
	}
	if len(compiled) != 1 || len(compiled[0].Patches) != 1 {
		t.Fatalf("expected one final patch, got %+v", compiled)
	}
	if got := compiled[0].Patches[0].Value; got != "system-protect" {
		t.Fatalf("expected system-protect value to win, got %s", got)
	}
	if summary.AppliedRuleCount != 1 {
		t.Fatalf("expected appliedRuleCount=1, got %d", summary.AppliedRuleCount)
	}
}

func TestCompileModifierRulesForInstance_VeleroNativePhase1TypeRejected(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{
		{
			ID:   "merge-not-supported",
			Mode: disasterv1.RestoreModifierModeVeleroNative,
			Conditions: disasterv1.Conditions{
				GroupResource: "deployments.apps",
			},
			VeleroRule: &disasterv1.RestoreModifierVeleroRule{
				MergePatches: []string{"kind: Deployment"},
			},
		},
	})

	_, _, err := compileModifierRulesForInstance(instance, ApplyInstanceRestorePolicyOptions{})
	if err == nil {
		t.Fatalf("expected rejection for merge patch, got nil")
	}
	if !strings.Contains(err.Error(), ModifierErrorRuleRejected) {
		t.Fatalf("expected %s, got %v", ModifierErrorRuleRejected, err)
	}
}

func TestCompileModifierRulesForInstance_ReversiblePairPlaceholderRespectsBaseline(t *testing.T) {
	t.Parallel()

	rules := []disasterv1.RestoreModifierRule{
		{
			ID:   "tpl",
			Mode: disasterv1.RestoreModifierModeReversible,
			Conditions: disasterv1.Conditions{
				GroupResource: "deployments.apps",
			},
			Pair: &disasterv1.RestoreModifierPair{
				Path:        "/spec/template/spec/containers/0/env/0/value",
				SourceValue: "mysql.{{ .SourceCluster }}.svc",
				TargetValue: "mysql.{{ .TargetCluster }}.svc",
			},
		},
	}

	instance := baseInstanceWithPolicy(true, rules)
	instance.Status.PrimaryCluster = "cluster-a"
	instance.Status.SecondaryCluster = "cluster-b"

	compiled, _, err := compileModifierRulesForInstance(
		instance,
		ApplyInstanceRestorePolicyOptions{
			BaselineSourceCluster: "cluster-a",
			BaselineTargetCluster: "cluster-b",
		},
	)
	if err != nil {
		t.Fatalf("unexpected pair placeholder compile error: %v", err)
	}
	if got := compiled[0].Patches[0].Value; got != "mysql.cluster-b.svc" {
		t.Fatalf("unexpected forward pair value: %s", got)
	}

	instance.Status.PrimaryCluster = "cluster-b"
	instance.Status.SecondaryCluster = "cluster-a"
	compiled, _, err = compileModifierRulesForInstance(
		instance,
		ApplyInstanceRestorePolicyOptions{
			BaselineSourceCluster: "cluster-a",
			BaselineTargetCluster: "cluster-b",
		},
	)
	if err != nil {
		t.Fatalf("unexpected reverse pair compile error: %v", err)
	}
	if got := compiled[0].Patches[0].Value; got != "mysql.cluster-a.svc" {
		t.Fatalf("unexpected reverse pair value: %s", got)
	}
}

func TestCompileModifierRulesForInstance_ReversiblePairPlaceholderRejectsUnsupportedTemplate(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{{
		ID:   "bad-placeholder",
		Mode: disasterv1.RestoreModifierModeReversible,
		Conditions: disasterv1.Conditions{
			GroupResource: "deployments.apps",
		},
		Pair: &disasterv1.RestoreModifierPair{
			Path:        "/spec/template/spec/containers/0/env/0/value",
			SourceValue: "mysql.{{ .SourceCluster }}.svc",
			TargetValue: "{{ printf \"%s\" .TargetCluster }}",
		},
	}})
	instance.Status.PrimaryCluster = "cluster-a"
	instance.Status.SecondaryCluster = "cluster-b"

	_, _, err := compileModifierRulesForInstance(
		instance,
		ApplyInstanceRestorePolicyOptions{
			BaselineSourceCluster: "cluster-a",
			BaselineTargetCluster: "cluster-b",
		},
	)
	if err == nil {
		t.Fatalf("expected unsupported placeholder rejection")
	}
	if !strings.Contains(err.Error(), ModifierErrorRuleRejected) {
		t.Fatalf("expected %s, got %v", ModifierErrorRuleRejected, err)
	}
	if !strings.Contains(err.Error(), "unsupported placeholder") {
		t.Fatalf("expected unsupported placeholder detail, got %v", err)
	}
}

func TestCompileModifierRulesForInstance_LegacyTransformInputRejected(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "legacy map",
			raw:  `{"id":"legacy-map","mode":"reversible","conditions":{"groupResource":"services"},"transform":{"type":"map","path":"/spec/ports/0/nodePort","mapping":{"30080":"32080"}}}`,
		},
		{
			name: "legacy template",
			raw:  `{"id":"legacy-template","mode":"reversible","conditions":{"groupResource":"deployments.apps"},"transform":{"type":"template","path":"/spec/template/spec/containers/0/env/0/value","valueTemplate":"mysql.{{ .TargetCluster }}.svc"}}`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var rule disasterv1.RestoreModifierRule
			if err := json.Unmarshal([]byte(tc.raw), &rule); err != nil {
				t.Fatalf("unexpected unmarshal error: %v", err)
			}

			instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{rule})
			instance.Status.PrimaryCluster = "cluster-a"
			instance.Status.SecondaryCluster = "cluster-b"

			_, _, err := compileModifierRulesForInstance(
				instance,
				ApplyInstanceRestorePolicyOptions{
					BaselineSourceCluster: "cluster-a",
					BaselineTargetCluster: "cluster-b",
				},
			)
			if err == nil {
				t.Fatalf("expected legacy input rejection")
			}
			if !strings.Contains(err.Error(), "pair canonical form") {
				t.Fatalf("expected pair canonical form guidance, got %v", err)
			}
		})
	}
}

func baseInstanceWithPolicy(enabled bool, rules []disasterv1.RestoreModifierRule) *disasterv1.DisasterInstance {
	return &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			RestorePolicy: &disasterv1.RestorePolicy{
				UseUnifiedDirectionResolver: boolPtr(enabled),
				ModifierRules:               rules,
			},
		},
	}
}

func boolPtr(v bool) *bool { return &v }
