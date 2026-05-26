package imagemapping

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

func TestResolveRegistryMappings(t *testing.T) {
	source := &disasterv1.Cluster{
		Spec: disasterv1.ClusterSpec{
			ImageSources: []disasterv1.ImageSource{
				{Name: "prod-main", Registry: "harbor.prod.local"},
				{Name: "prod-third", Registry: "registry-1.docker.io"},
			},
		},
	}
	target := &disasterv1.Cluster{
		Spec: disasterv1.ClusterSpec{
			ImageSources: []disasterv1.ImageSource{
				{Name: "dr-main", Registry: "harbor.dr.local"},
				{Name: "dr-third", Registry: "registry-cache.dr.local"},
			},
		},
	}

	imageRewrite := &disasterv1.ImageRewriteConfig{
		Enabled:         true,
		UnmatchedPolicy: disasterv1.ImageRewriteUnmatchedPolicyFail,
		ApplyTo:         []disasterv1.ImageRewriteApplyTarget{disasterv1.ImageRewriteApplyResourceSync},
		Mappings: []disasterv1.ImageSourceMapping{
			{SourceImageSource: "prod-main", TargetImageSource: "dr-main"},
			{SourceImageSource: "prod-third", TargetImageSource: "dr-third"},
		},
	}

	mappings, policy, enabled, err := ResolveRegistryMappings(source, target, imageRewrite, disasterv1.ImageRewriteApplyResourceSync)
	if err != nil {
		t.Fatalf("ResolveRegistryMappings returned error: %v", err)
	}
	if !enabled {
		t.Fatalf("expected enabled=true")
	}
	if policy != disasterv1.ImageRewriteUnmatchedPolicyFail {
		t.Fatalf("expected policy Fail, got %s", policy)
	}
	if len(mappings) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(mappings))
	}
	if mappings[0].SourceRegistry != "harbor.prod.local" || mappings[0].TargetRegistry != "harbor.dr.local" {
		t.Fatalf("unexpected resolved mapping: %+v", mappings[0])
	}
}

func TestResolveRegistryMappings_ApplyToNotMatched(t *testing.T) {
	imageRewrite := &disasterv1.ImageRewriteConfig{
		Enabled: true,
		ApplyTo: []disasterv1.ImageRewriteApplyTarget{disasterv1.ImageRewriteApplyResourceSync},
		Mappings: []disasterv1.ImageSourceMapping{
			{SourceImageSource: "a", TargetImageSource: "b"},
		},
	}

	_, _, enabled, err := ResolveRegistryMappings(&disasterv1.Cluster{}, &disasterv1.Cluster{}, imageRewrite, disasterv1.ImageRewriteApplyDrill)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if enabled {
		t.Fatalf("expected enabled=false when apply target is not selected")
	}
}

func TestResolveRegistryMappings_RoleSwitchUsesCurrentDirection(t *testing.T) {
	clusterA := &disasterv1.Cluster{
		Spec: disasterv1.ClusterSpec{
			ImageSources: []disasterv1.ImageSource{
				{Name: "prod-main", Registry: "harbor.a.local"},
			},
		},
	}
	clusterB := &disasterv1.Cluster{
		Spec: disasterv1.ClusterSpec{
			ImageSources: []disasterv1.ImageSource{
				{Name: "dr-main", Registry: "harbor.b.local"},
			},
		},
	}
	imageRewrite := &disasterv1.ImageRewriteConfig{
		Enabled: true,
		ApplyTo: []disasterv1.ImageRewriteApplyTarget{
			disasterv1.ImageRewriteApplyResourceSync,
		},
		Mappings: []disasterv1.ImageSourceMapping{
			{SourceImageSource: "prod-main", TargetImageSource: "dr-main"},
		},
	}

	preMappings, _, enabled, err := ResolveRegistryMappings(
		clusterA,
		clusterB,
		imageRewrite,
		disasterv1.ImageRewriteApplyResourceSync,
	)
	if err != nil {
		t.Fatalf("ResolveRegistryMappings pre-failover returned error: %v", err)
	}
	if !enabled {
		t.Fatalf("expected pre-failover mapping enabled")
	}
	if len(preMappings) != 1 {
		t.Fatalf("expected 1 pre-failover mapping, got %d", len(preMappings))
	}
	if preMappings[0].SourceRegistry != "harbor.a.local" || preMappings[0].TargetRegistry != "harbor.b.local" {
		t.Fatalf("unexpected pre-failover mapping: %+v", preMappings[0])
	}

	postMappings, _, enabled, err := ResolveRegistryMappings(
		clusterB,
		clusterA,
		imageRewrite,
		disasterv1.ImageRewriteApplyResourceSync,
	)
	if err != nil {
		t.Fatalf("ResolveRegistryMappings post-failover returned error: %v", err)
	}
	if !enabled {
		t.Fatalf("expected post-failover mapping enabled")
	}
	if len(postMappings) != 1 {
		t.Fatalf("expected 1 post-failover mapping, got %d", len(postMappings))
	}
	if postMappings[0].SourceRegistry != "harbor.b.local" || postMappings[0].TargetRegistry != "harbor.a.local" {
		t.Fatalf("unexpected post-failover mapping: %+v", postMappings[0])
	}
}

func TestRewriteImage_LongestPrefixWins(t *testing.T) {
	mappings := []RegistryMapping{
		{SourceRegistry: "harbor.prod.local", TargetRegistry: "dr.local/root"},
		{SourceRegistry: "harbor.prod.local/team-a", TargetRegistry: "dr.local/team-a"},
	}
	rewritten, matched := RewriteImage("harbor.prod.local/team-a/app:v1", mappings)
	if !matched {
		t.Fatalf("expected image matched")
	}
	if rewritten != "dr.local/team-a/app:v1" {
		t.Fatalf("unexpected rewritten image: %s", rewritten)
	}
}

func TestBuildRulesFromWorkloads_CoversContainersAndInitContainers(t *testing.T) {
	deploy := appsv1.Deployment{}
	deploy.Namespace = "demo"
	deploy.Name = "demo-deploy"
	deploy.Spec.Template.Spec.Containers = []corev1.Container{
		{Name: "main", Image: "harbor.prod.local/app/main:v1"},
		{Name: "sidecar", Image: "harbor.prod.local/app/sidecar:v1"},
	}
	deploy.Spec.Template.Spec.InitContainers = []corev1.Container{
		{Name: "init-a", Image: "harbor.prod.local/app/init:v1"},
	}

	rules, unmatched := BuildRulesFromWorkloads(
		[]appsv1.Deployment{deploy},
		nil,
		[]RegistryMapping{{SourceRegistry: "harbor.prod.local", TargetRegistry: "harbor.dr.local"}},
		disasterv1.ImageRewriteUnmatchedPolicyFail,
	)

	if len(unmatched) != 0 {
		t.Fatalf("expected no unmatched, got %+v", unmatched)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if len(rules[0].Patches) != 3 {
		t.Fatalf("expected 3 image patches, got %d", len(rules[0].Patches))
	}
}

func TestBuildRulesFromWorkloads_UnmatchedFail(t *testing.T) {
	sts := appsv1.StatefulSet{}
	sts.Namespace = "demo"
	sts.Name = "db"
	sts.Spec.Template.Spec.Containers = []corev1.Container{
		{Name: "db", Image: "unknown.registry.local/db:v1"},
	}

	rules, unmatched := BuildRulesFromWorkloads(
		nil,
		[]appsv1.StatefulSet{sts},
		[]RegistryMapping{{SourceRegistry: "harbor.prod.local", TargetRegistry: "harbor.dr.local"}},
		disasterv1.ImageRewriteUnmatchedPolicyFail,
	)

	if len(rules) != 0 {
		t.Fatalf("expected no patch rules when image is unmatched, got %d", len(rules))
	}
	if len(unmatched) != 1 {
		t.Fatalf("expected 1 unmatched image, got %d (%v)", len(unmatched), unmatched)
	}
}
