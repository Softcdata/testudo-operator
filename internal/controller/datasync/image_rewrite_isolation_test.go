package datasync

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/metadata"
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
	if len(rule.Patches) != 8 {
		t.Fatalf("expected 8 trafficless patches, got %d", len(rule.Patches))
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
	trafficlessLabels := map[string]string{}
	if err := json.Unmarshal([]byte(patchByPath["/metadata/labels"].Value), &trafficlessLabels); err != nil {
		t.Fatalf("decode labels patch: %v", err)
	}
	if trafficlessLabels["trafficless"] != "true" {
		t.Fatalf("expected trafficless label, got %#v", trafficlessLabels)
	}
	if trafficlessLabels[metadata.LabelTrafficlessLifecycle] != metadata.TrafficlessLifecycleDataSync {
		t.Fatalf("expected DataSync trafficless lifecycle label, got %#v", trafficlessLabels)
	}
	for _, key := range []string{
		metadata.LabelCleanupManagedBy,
		metadata.LabelCleanupOwnerToken,
		metadata.LabelCleanupRelation,
		metadata.LabelCleanupStrategy,
		metadata.LabelTrafficlessRun,
	} {
		if trafficlessLabels[key] == "" {
			t.Fatalf("expected scoped cleanup label %s, got %#v", key, trafficlessLabels)
		}
	}
	if patchByPath["/spec/nodeName"].Operation != "add" || patchByPath["/spec/nodeName"].Value != "" {
		t.Fatalf("unexpected nodeName cleanup patch: %#v", patchByPath["/spec/nodeName"])
	}
	if patchByPath["/spec/nodeSelector"].Operation != "add" || patchByPath["/spec/nodeSelector"].Value != "{}" {
		t.Fatalf("unexpected nodeSelector cleanup patch: %#v", patchByPath["/spec/nodeSelector"])
	}
	if patchByPath["/spec/affinity"].Operation != "add" || patchByPath["/spec/affinity"].Value != "{}" {
		t.Fatalf("unexpected affinity cleanup patch: %#v", patchByPath["/spec/affinity"])
	}
	if _, ok := patchByPath["/spec/topologySpreadConstraints"]; ok {
		t.Fatalf("did not expect topologySpreadConstraints cleanup patch")
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
