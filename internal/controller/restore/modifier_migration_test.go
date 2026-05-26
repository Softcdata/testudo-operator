package restore

import (
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

func TestImportVeleroResourceModifierRules(t *testing.T) {
	t.Parallel()

	rules, err := ImportVeleroResourceModifierRules(
		[]disasterv1.ResourceModifierRule{
			{
				Conditions: disasterv1.Conditions{
					GroupResource: "deployments.apps",
					Namespaces:    []string{"app"},
				},
				Patches: []disasterv1.JSONPatch{{
					Operation: "add",
					Path:      "/metadata/annotations/patched-by",
					Value:     "platform",
				}},
			},
		},
		VeleroRuleImportOptions{
			ApplyTo:  []disasterv1.RestoreModifierApplyTarget{disasterv1.RestoreModifierApplyResourceSync},
			Priority: 300,
			IDPrefix: "legacy",
		},
	)
	if err != nil {
		t.Fatalf("unexpected import error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 converted rule, got %d", len(rules))
	}

	r := rules[0]
	if r.ID != "legacy-0000" {
		t.Fatalf("unexpected rule id: %s", r.ID)
	}
	if r.Mode != disasterv1.RestoreModifierModeVeleroNative {
		t.Fatalf("unexpected mode: %s", r.Mode)
	}
	if r.Priority != 300 {
		t.Fatalf("unexpected priority: %d", r.Priority)
	}
	if len(r.ApplyTo) != 1 || r.ApplyTo[0] != disasterv1.RestoreModifierApplyResourceSync {
		t.Fatalf("unexpected applyTo: %+v", r.ApplyTo)
	}
	if r.VeleroRule == nil || len(r.VeleroRule.Patches) != 1 {
		t.Fatalf("expected velero patches, got %+v", r.VeleroRule)
	}
	if r.VeleroRule.Patches[0].Path != "/metadata/annotations/patched-by" {
		t.Fatalf("unexpected patch path: %s", r.VeleroRule.Patches[0].Path)
	}
}

func TestConvertLegacyRestorePolicyToDSL(t *testing.T) {
	t.Parallel()

	policy := &disasterv1.RestorePolicy{
		StorageClassMapping: &disasterv1.RestoreClassMappingPolicy{
			Mappings: []disasterv1.RestoreClassMapping{{
				SourceClass: "sc-main",
				TargetClass: "sc-dr",
			}},
		},
		IngressClassMapping: &disasterv1.RestoreClassMappingPolicy{
			Mappings: []disasterv1.RestoreClassMapping{{
				SourceClass: "ing-main",
				TargetClass: "ing-dr",
			}},
		},
	}

	result, err := ConvertLegacyRestorePolicyToDSL(policy, []string{"app-ns"})
	if err != nil {
		t.Fatalf("unexpected convert error: %v", err)
	}
	if len(result.Rules) != 3 {
		t.Fatalf("expected 3 converted rules (pvc+pv+ingress), got %d", len(result.Rules))
	}

	var gotPVC, gotPV, gotIngress bool
	for _, rule := range result.Rules {
		if rule.Mode != disasterv1.RestoreModifierModeReversible || rule.Pair == nil {
			t.Fatalf("expected reversible pair rule, got %+v", rule)
		}
		switch rule.Conditions.GroupResource {
		case "persistentvolumeclaims":
			gotPVC = true
			if len(rule.Conditions.Namespaces) != 1 || rule.Conditions.Namespaces[0] != "app-ns" {
				t.Fatalf("unexpected pvc namespaces: %+v", rule.Conditions.Namespaces)
			}
			if rule.Pair.SourceValue != "sc-main" || rule.Pair.TargetValue != "sc-dr" {
				t.Fatalf("unexpected pvc pair: %+v", rule.Pair)
			}
		case "persistentvolumes":
			gotPV = true
			if rule.Pair.SourceValue != "sc-main" || rule.Pair.TargetValue != "sc-dr" {
				t.Fatalf("unexpected pv pair: %+v", rule.Pair)
			}
		case "ingresses.networking.k8s.io":
			gotIngress = true
			if len(rule.Conditions.Namespaces) != 1 || rule.Conditions.Namespaces[0] != "app-ns" {
				t.Fatalf("unexpected ingress namespaces: %+v", rule.Conditions.Namespaces)
			}
			if rule.Pair.SourceValue != "ing-main" || rule.Pair.TargetValue != "ing-dr" {
				t.Fatalf("unexpected ingress pair: %+v", rule.Pair)
			}
		}
	}
	if !gotPVC || !gotPV || !gotIngress {
		t.Fatalf("missing converted rules: pvc=%v pv=%v ingress=%v", gotPVC, gotPV, gotIngress)
	}
}

func TestConvertLegacyRestorePolicyToDSL_MultiplePVTargetsRejected(t *testing.T) {
	t.Parallel()

	policy := &disasterv1.RestorePolicy{
		StorageClassMapping: &disasterv1.RestoreClassMappingPolicy{
			Mappings: []disasterv1.RestoreClassMapping{
				{SourceClass: "sc-a", TargetClass: "sc-dr-a"},
				{SourceClass: "sc-b", TargetClass: "sc-dr-b"},
			},
		},
	}

	_, err := ConvertLegacyRestorePolicyToDSL(policy, []string{"app-ns"})
	if err == nil {
		t.Fatalf("expected convert rejection for multiple PV target classes")
	}
}
