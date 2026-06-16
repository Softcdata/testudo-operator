package v1

import (
	"encoding/json"
	"testing"
)

func TestRestorePolicyBulkModifierFieldsJSONRoundTrip(t *testing.T) {
	t.Parallel()

	policy := RestorePolicy{
		BulkModifierActions: []BulkModifierAction{{
			ID:          "replace-ip",
			Action:      BulkModifierActionReplaceExactValue,
			Enabled:     boolPtr(true),
			ApplyTo:     []RestoreModifierApplyTarget{RestoreModifierApplyResourceSync},
			SourceValue: "10.10.0.12",
			TargetValue: "10.20.0.12",
		}},
		ModifierRuleSnapshot: []RestoreModifierRule{{
			ID:   "bulk-replace-ip-0001",
			Mode: RestoreModifierModeReversible,
			Conditions: Conditions{
				GroupResource:     "deployments.apps",
				ResourceNameRegex: "^demo$",
				Namespaces:        []string{"demo-ns"},
			},
			Pair: &RestoreModifierPair{
				Path:        "/metadata/annotations/site-role",
				SourceValue: "secondary",
				TargetValue: "primary",
			},
		}},
		ModifierRuleSnapshotHash: "sha256:test",
	}

	payload, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("expected marshal success, got %v", err)
	}

	var decoded RestorePolicy
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("expected unmarshal success, got %v", err)
	}

	if len(decoded.BulkModifierActions) != 1 {
		t.Fatalf("expected 1 bulk action, got %d", len(decoded.BulkModifierActions))
	}
	if decoded.BulkModifierActions[0].Action != BulkModifierActionReplaceExactValue {
		t.Fatalf("expected bulk action replaceExactValue, got %s", decoded.BulkModifierActions[0].Action)
	}
	if decoded.BulkModifierActions[0].Enabled == nil || !*decoded.BulkModifierActions[0].Enabled {
		t.Fatalf("expected enabled=true round-trip, got %v", decoded.BulkModifierActions[0].Enabled)
	}
	if len(decoded.ModifierRuleSnapshot) != 1 {
		t.Fatalf("expected 1 snapshot rule, got %d", len(decoded.ModifierRuleSnapshot))
	}
	if decoded.ModifierRuleSnapshot[0].Pair == nil || decoded.ModifierRuleSnapshot[0].Pair.Path != "/metadata/annotations/site-role" {
		t.Fatalf("expected snapshot pair path round-trip, got %#v", decoded.ModifierRuleSnapshot[0].Pair)
	}
	if decoded.ModifierRuleSnapshotHash != "sha256:test" {
		t.Fatalf("expected snapshot hash round-trip, got %s", decoded.ModifierRuleSnapshotHash)
	}
}

func TestRestorePolicyRewriteImageBulkModifierJSONRoundTrip(t *testing.T) {
	t.Parallel()

	policy := RestorePolicy{
		BulkModifierActions: []BulkModifierAction{{
			ID:              "rewrite-primary-registry",
			Action:          BulkModifierActionRewriteImage,
			Enabled:         boolPtr(true),
			ApplyTo:         []RestoreModifierApplyTarget{RestoreModifierApplyResourceSync, RestoreModifierApplyDrill},
			DirectionPolicy: RestoreModifierDirectionPolicyAuto,
			ImageRewrite: &DynamicImageRewriteConfig{
				SourcePrefix:    "10.11.11.1:5000/",
				TargetPrefix:    "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/",
				UnmatchedPolicy: ImageRewriteUnmatchedPolicyKeep,
				DigestPolicy:    ImageRewriteDigestPolicyPreserve,
			},
		}},
	}

	payload, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("expected marshal success, got %v", err)
	}

	var decoded RestorePolicy
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("expected unmarshal success, got %v", err)
	}

	if len(decoded.BulkModifierActions) != 1 {
		t.Fatalf("expected 1 bulk action, got %d", len(decoded.BulkModifierActions))
	}
	action := decoded.BulkModifierActions[0]
	if action.Action != BulkModifierActionRewriteImage {
		t.Fatalf("expected action rewriteImage, got %s", action.Action)
	}
	if action.ImageRewrite == nil {
		t.Fatalf("expected imageRewrite round-trip")
	}
	if action.ImageRewrite.SourcePrefix != "10.11.11.1:5000/" {
		t.Fatalf("unexpected sourcePrefix: %s", action.ImageRewrite.SourcePrefix)
	}
	if action.ImageRewrite.TargetPrefix != "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/" {
		t.Fatalf("unexpected targetPrefix: %s", action.ImageRewrite.TargetPrefix)
	}
	if action.ImageRewrite.DigestPolicy != ImageRewriteDigestPolicyPreserve {
		t.Fatalf("unexpected digestPolicy: %s", action.ImageRewrite.DigestPolicy)
	}
}

func TestRestorePolicyDeepCopyPreservesBulkModifierFields(t *testing.T) {
	t.Parallel()

	original := &RestorePolicy{
		BulkModifierActions: []BulkModifierAction{{
			ID:      "drop-key",
			Action:  BulkModifierActionRemoveKey,
			Enabled: boolPtr(true),
			Key:     "site-role",
		}, {
			ID:      "rewrite-primary-registry",
			Action:  BulkModifierActionRewriteImage,
			Enabled: boolPtr(true),
			ImageRewrite: &DynamicImageRewriteConfig{
				SourcePrefix: "10.11.11.1:5000/",
				TargetPrefix: "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/",
			},
		}},
		ModifierRuleSnapshot: []RestoreModifierRule{{
			ID:   "bulk-drop-key-0001",
			Mode: RestoreModifierModeVeleroNative,
			VeleroRule: &RestoreModifierVeleroRule{
				Patches: []JSONPatch{{
					Operation: "remove",
					Path:      "/metadata/annotations/site-role",
				}},
			},
		}},
		ModifierRuleSnapshotHash: "sha256:before",
	}

	cloned := original.DeepCopy()
	cloned.BulkModifierActions[0].ID = "mutated"
	cloned.BulkModifierActions[1].ImageRewrite.TargetPrefix = "registry-mutated.example.com/"
	cloned.ModifierRuleSnapshot[0].VeleroRule.Patches[0].Path = "/metadata/annotations/changed"
	cloned.ModifierRuleSnapshotHash = "sha256:after"

	if original.BulkModifierActions[0].ID != "drop-key" {
		t.Fatalf("expected bulk action ID to remain unchanged, got %s", original.BulkModifierActions[0].ID)
	}
	if original.BulkModifierActions[1].ImageRewrite == nil || original.BulkModifierActions[1].ImageRewrite.TargetPrefix != "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/" {
		t.Fatalf("expected imageRewrite targetPrefix to remain unchanged, got %#v", original.BulkModifierActions[1].ImageRewrite)
	}
	if original.ModifierRuleSnapshot[0].VeleroRule.Patches[0].Path != "/metadata/annotations/site-role" {
		t.Fatalf("expected snapshot patch path to remain unchanged, got %s", original.ModifierRuleSnapshot[0].VeleroRule.Patches[0].Path)
	}
	if original.ModifierRuleSnapshotHash != "sha256:before" {
		t.Fatalf("expected snapshot hash to remain unchanged, got %s", original.ModifierRuleSnapshotHash)
	}
}

func boolPtr(v bool) *bool { return &v }
