package datasync

import (
	"context"
	"strings"
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

func TestBuildAppRestoreSpec_KeepsTrafficlessSemanticsWhenImageRewriteEnabled(t *testing.T) {
	reconciler := &DataSyncReconciler{}

	ds := &disasterv1.DataSync{
		Spec: disasterv1.DataSyncSpec{
			TrafficlessConfig: &disasterv1.TrafficlessConfig{
				Image:   "registry.local/tools/trafficless:v2",
				Command: []string{"sleep", "7200"},
			},
		},
	}

	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app-ns"},
		},
		Status: disasterv1.DisasterInstanceStatus{
			PrimaryCluster:   "cluster-a",
			SecondaryCluster: "cluster-b",
		},
	}

	config := &disasterv1.DisasterConfig{
		Spec: disasterv1.DisasterConfigSpec{
			SourceCluster:      "cluster-a",
			TargetCluster:      "cluster-b",
			StorageRepository:  "repo-main",
			ResourceSyncPolicy: "resource-policy",
		},
	}

	spec, _, err := reconciler.buildAppRestoreSpec(context.Background(), ds, config, instance, "backup-001")
	if err != nil {
		t.Fatalf("buildAppRestoreSpec returned error: %v", err)
	}

	if len(spec.ResourceModifierRules) != 1 {
		t.Fatalf("expected only trafficless modifier rule, got %d", len(spec.ResourceModifierRules))
	}

	rule := spec.ResourceModifierRules[0]
	if rule.Conditions.GroupResource != "pods" {
		t.Fatalf("expected trafficless rule for pods, got %s", rule.Conditions.GroupResource)
	}
	if len(rule.Patches) != 5 {
		t.Fatalf("expected 5 trafficless patches, got %d", len(rule.Patches))
	}

	patchByPath := map[string]disasterv1.JSONPatch{}
	for _, patch := range rule.Patches {
		patchByPath[patch.Path] = patch
		if strings.HasPrefix(patch.Path, "/spec/template/spec/") {
			t.Fatalf("unexpected workload image rewrite patch in datasync restore: %s", patch.Path)
		}
	}

	if patchByPath["/spec/containers/0/image"].Value != "registry.local/tools/trafficless:v2" {
		t.Fatalf("unexpected trafficless image patch: %s", patchByPath["/spec/containers/0/image"].Value)
	}
	if patchByPath["/spec/containers/0/command"].Value != `["sleep","7200"]` {
		t.Fatalf("unexpected trafficless command patch: %s", patchByPath["/spec/containers/0/command"].Value)
	}
	if patchByPath["/spec/containers/0/args"].Value != "[]" {
		t.Fatalf("unexpected trafficless args patch: %s", patchByPath["/spec/containers/0/args"].Value)
	}
	if patchByPath["/metadata/labels"].Value != `{"trafficless": "true"}` {
		t.Fatalf("unexpected labels patch: %s", patchByPath["/metadata/labels"].Value)
	}

	ownerRefPatch, ok := patchByPath["/metadata/ownerReferences"]
	if !ok {
		t.Fatalf("expected ownerReferences patch")
	}
	if ownerRefPatch.Operation != "add" {
		t.Fatalf("expected ownerReferences patch operation add, got %s", ownerRefPatch.Operation)
	}
	if ownerRefPatch.Value != "[]" {
		t.Fatalf("expected ownerReferences patch value [] , got %s", ownerRefPatch.Value)
	}
}
