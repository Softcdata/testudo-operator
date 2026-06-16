package restore

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/softcdata/testudo-operator/internal/controller/imagemapping"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

func TestBuildAppRestoreSpec_ResourceRestoreIncludesImageRulesForMultiContainer(t *testing.T) {
	deploy := appsv1.Deployment{}
	deploy.Namespace = "demo"
	deploy.Name = "web"
	deploy.Spec.Template.Spec.Containers = []corev1.Container{
		{Name: "main", Image: "harbor.prod.local/team/web:v1"},
		{Name: "sidecar", Image: "harbor.prod.local/team/sidecar:v1"},
	}
	deploy.Spec.Template.Spec.InitContainers = []corev1.Container{
		{Name: "init-db", Image: "harbor.prod.local/team/init-db:v1"},
	}

	imageRules, unmatched := imagemapping.BuildRulesFromWorkloads(
		[]appsv1.Deployment{deploy},
		nil,
		[]imagemapping.RegistryMapping{
			{
				SourceRegistry: "harbor.prod.local",
				TargetRegistry: "harbor.dr.local",
			},
		},
		disasterv1.ImageRewriteUnmatchedPolicyFail,
	)
	if len(unmatched) != 0 {
		t.Fatalf("expected no unmatched image, got %+v", unmatched)
	}
	if len(imageRules) != 1 {
		t.Fatalf("expected 1 image rewrite rule, got %d", len(imageRules))
	}

	spec := BuildAppRestoreSpec(BuilderConfig{
		RestoreType:                RestoreTypeResource,
		BackupSource:               "rs-demo",
		BackupName:                 "backup-001",
		TargetCluster:              "cluster-b",
		SourceCluster:              "cluster-a",
		StorageRepository:          "repo-main",
		IncludedNamespaces:         []string{"demo"},
		ExtraResourceModifierRules: imageRules,
	})

	// 2 条骨架规则 + 1 条 workload 级镜像规则
	if len(spec.ResourceModifierRules) != 3 {
		t.Fatalf("expected 3 resource modifier rules, got %d", len(spec.ResourceModifierRules))
	}

	var imageRule *disasterv1.ResourceModifierRule
	for i := range spec.ResourceModifierRules {
		rule := &spec.ResourceModifierRules[i]
		if rule.Conditions.GroupResource == "deployments.apps" && rule.Conditions.ResourceNameRegex == "^web$" {
			imageRule = rule
			break
		}
	}
	if imageRule == nil {
		t.Fatalf("expected one deployment image rule for workload web")
	}
	if len(imageRule.Patches) != 3 {
		t.Fatalf("expected 3 image patches for containers+initContainers, got %d", len(imageRule.Patches))
	}

	patchByPath := map[string]string{}
	for _, patch := range imageRule.Patches {
		patchByPath[patch.Path] = patch.Value
	}

	if patchByPath["/spec/template/spec/containers/0/image"] != "harbor.dr.local/team/web:v1" {
		t.Fatalf("unexpected patch for main container: %s", patchByPath["/spec/template/spec/containers/0/image"])
	}
	if patchByPath["/spec/template/spec/containers/1/image"] != "harbor.dr.local/team/sidecar:v1" {
		t.Fatalf("unexpected patch for sidecar container: %s", patchByPath["/spec/template/spec/containers/1/image"])
	}
	if patchByPath["/spec/template/spec/initContainers/0/image"] != "harbor.dr.local/team/init-db:v1" {
		t.Fatalf("unexpected patch for init container: %s", patchByPath["/spec/template/spec/initContainers/0/image"])
	}
}

func TestBuildAppRestoreSpec_DataRestoreUsesIdempotentOwnerReferencesPatch(t *testing.T) {
	spec := BuildAppRestoreSpec(BuilderConfig{
		RestoreType:        RestoreTypeData,
		BackupSource:       "ds-demo",
		BackupName:         "backup-001",
		TargetCluster:      "cluster-b",
		SourceCluster:      "cluster-a",
		StorageRepository:  "repo-main",
		IncludedNamespaces: []string{"demo"},
		IsForDrill:         false,
	})

	if len(spec.ResourceModifierRules) != 1 {
		t.Fatalf("expected one trafficless modifier rule, got %d", len(spec.ResourceModifierRules))
	}
	if spec.ResourceModifierRules[0].Conditions.GroupResource != "pods" {
		t.Fatalf("expected pods modifier rule, got %s", spec.ResourceModifierRules[0].Conditions.GroupResource)
	}

	var ownerRefPatch *disasterv1.JSONPatch
	for i := range spec.ResourceModifierRules[0].Patches {
		patch := &spec.ResourceModifierRules[0].Patches[i]
		if patch.Path == "/metadata/ownerReferences" {
			ownerRefPatch = patch
			break
		}
	}
	if ownerRefPatch == nil {
		t.Fatalf("expected ownerReferences patch in trafficless modifiers")
	}
	if ownerRefPatch.Operation != "add" {
		t.Fatalf("expected ownerReferences operation add, got %s", ownerRefPatch.Operation)
	}
	if ownerRefPatch.Value != "[]" {
		t.Fatalf("expected ownerReferences value [] , got %s", ownerRefPatch.Value)
	}
}

func TestMakePVCVolumeNameCleanupRule(t *testing.T) {
	inputNamespaces := []string{"ns-a", "ns-b"}
	rule := MakePVCVolumeNameCleanupRule(inputNamespaces)

	if rule.Conditions.GroupResource != "persistentvolumeclaims" {
		t.Fatalf("expected groupResource persistentvolumeclaims, got %s", rule.Conditions.GroupResource)
	}
	if len(rule.Conditions.Namespaces) != 2 || rule.Conditions.Namespaces[0] != "ns-a" || rule.Conditions.Namespaces[1] != "ns-b" {
		t.Fatalf("unexpected namespaces: %#v", rule.Conditions.Namespaces)
	}
	if len(rule.Patches) != 1 {
		t.Fatalf("expected exactly 1 patch, got %d", len(rule.Patches))
	}
	if rule.Patches[0].Operation != "add" {
		t.Fatalf("expected add operation, got %s", rule.Patches[0].Operation)
	}
	if rule.Patches[0].Path != "/spec/volumeName" {
		t.Fatalf("expected /spec/volumeName path, got %s", rule.Patches[0].Path)
	}
	if rule.Patches[0].Value != "" {
		t.Fatalf("expected empty volumeName value, got %q", rule.Patches[0].Value)
	}

	// Ensure input slice mutation does not affect built rule.
	inputNamespaces[0] = "mutated"
	if rule.Conditions.Namespaces[0] != "ns-a" {
		t.Fatalf("expected namespace copy isolation, got %#v", rule.Conditions.Namespaces)
	}
}
