package metadata

import "testing"

func TestBuildUpstreamLabelKeyFromTargetLabels(t *testing.T) {
	labels := map[string]string{
		LabelDependencyToken: "ABCD-0011",
	}

	key, ok := BuildUpstreamLabelKeyFromTargetLabels(labels)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	want := LabelDependencyToPrefix + "abcd0011"
	if key != want {
		t.Fatalf("expected key %q, got %q", want, key)
	}
}

func TestBuildUpstreamLabelKeyFromTargetLabelsMissing(t *testing.T) {
	key, ok := BuildUpstreamLabelKeyFromTargetLabels(map[string]string{})
	if ok {
		t.Fatalf("expected ok=false, got key=%q", key)
	}
}

func TestParseDownstreamFromLabels(t *testing.T) {
	labels := map[string]string{
		"a":                                       "b",
		LabelDependencyToPrefix + "ff00GG11":      " spec/config ",
		LabelDependencyToPrefix + "0011aa22":      "spec.cluster",
		LabelDependencyToPrefix + "____":          "bad",
		LabelDependencyToPrefix + "abcdefabcdef1": "",
	}

	edges := ParseDownstreamFromLabels(labels)
	if len(edges) != 3 {
		t.Fatalf("expected 3 edges, got %d: %+v", len(edges), edges)
	}

	if edges[0].TargetToken != "0011aa22" || edges[0].RelationCode != "spec.cluster" {
		t.Fatalf("unexpected first edge: %+v", edges[0])
	}
	if edges[1].TargetToken != "abcdefabcdef1" || edges[1].RelationCode != "unknown" {
		t.Fatalf("unexpected second edge: %+v", edges[1])
	}
	if edges[2].TargetToken != "ff0011" || edges[2].RelationCode != "spec-config" {
		t.Fatalf("unexpected third edge: %+v", edges[2])
	}
}

func TestCanDeleteFromUpstreamCount(t *testing.T) {
	if !CanDeleteFromUpstreamCount(0) {
		t.Fatalf("expected can delete when upstream=0")
	}
	if CanDeleteFromUpstreamCount(1) {
		t.Fatalf("expected cannot delete when upstream>0")
	}
}
