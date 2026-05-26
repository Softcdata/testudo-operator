package metadata

import "testing"

func TestBuildDependencyTokenStable(t *testing.T) {
	uid := "2f5c1c3e-8e4a-4f5d-8e4d-c4b8f93e4c91"
	token1 := BuildDependencyToken(uid)
	token2 := BuildDependencyToken(uid)
	if token1 == "" {
		t.Fatalf("expected token, got empty")
	}
	if token1 != token2 {
		t.Fatalf("expected stable token, got %s vs %s", token1, token2)
	}
	if len(token1) != 16 {
		t.Fatalf("expected token length 16, got %d", len(token1))
	}
}

func TestEnsureDependencyTokenLabel(t *testing.T) {
	labels, token, changed := EnsureDependencyTokenLabel(nil, "uid-1")
	if !changed {
		t.Fatalf("expected changed=true on first set")
	}
	if token == "" {
		t.Fatalf("expected non-empty token")
	}
	if labels[LabelDependencyToken] != token {
		t.Fatalf("expected label token %s, got %s", token, labels[LabelDependencyToken])
	}

	_, token2, changed2 := EnsureDependencyTokenLabel(labels, "uid-1")
	if changed2 {
		t.Fatalf("expected no change on same uid")
	}
	if token2 != token {
		t.Fatalf("expected same token, got %s vs %s", token, token2)
	}
}

func TestRebuildDependencyToLabels(t *testing.T) {
	labels := map[string]string{
		"a": "b",
		LabelDependencyToPrefix + "deadbeefdeadbeef":     "spec.old",
		LabelDependencyToPrefix + "abcdefabcdefabcdabcd": "spec.keep",
	}
	edges := []DependencyEdge{
		{TargetToken: "abcdefabcdefabcdabcd", RelationCode: "spec.keep"},
		{TargetToken: "ff00112233445566", RelationCode: "spec.config"},
	}

	out, changed := RebuildDependencyToLabels(labels, edges)
	if !changed {
		t.Fatalf("expected changed=true")
	}

	if _, ok := out[LabelDependencyToPrefix+"deadbeefdeadbeef"]; ok {
		t.Fatalf("old edge should be removed")
	}
	if out[LabelDependencyToPrefix+"abcdefabcdefabcdabcd"] != "spec.keep" {
		t.Fatalf("expected kept edge")
	}
	if out[LabelDependencyToPrefix+"ff00112233445566"] != "spec.config" {
		t.Fatalf("expected new edge")
	}
	if out["a"] != "b" {
		t.Fatalf("non dependency labels should be preserved")
	}
}

func TestNormalizeRelationCode(t *testing.T) {
	got := NormalizeRelationCode(" spec/config:field ")
	if got != "spec-config-field" {
		t.Fatalf("unexpected normalized code: %s", got)
	}

	if NormalizeRelationCode("") != "unknown" {
		t.Fatalf("empty relation should fallback to unknown")
	}
}
