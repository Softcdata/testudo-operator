package restore

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

func TestCompileDynamicImageRewriteRules_UsesCurrentSourceImage(t *testing.T) {
	t.Parallel()

	source := newDynamicImageRewriteFakeClient(t,
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "bkcmdb-sync", Namespace: "blueking"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "sync",
							Image: "10.11.11.1:5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0",
						}},
					},
				},
			},
		},
	)
	instance := dynamicImageRewriteInstance("cluster-a", "cluster-b", []disasterv1.BulkModifierAction{dynamicImageRewriteAction(
		"rewrite-primary",
		"10.11.11.1:5000/",
		"registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/",
		disasterv1.ImageRewriteUnmatchedPolicyKeep,
	)})

	rules, summary, err := (&DynamicImageRewriteCompiler{}).CompileDynamicImageRewriteRules(
		context.Background(),
		instance,
		source,
		disasterv1.RestoreModifierApplyResourceSync,
		WithDynamicImageRewriteBaseline("cluster-a", "cluster-b"),
	)
	if err != nil {
		t.Fatalf("CompileDynamicImageRewriteRules returned error: %v", err)
	}
	if summary.GeneratedRuleCount != 1 || summary.MatchedImageCount != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if len(rules) != 1 {
		t.Fatalf("expected one runtime rule, got %d", len(rules))
	}
	rule := rules[0]
	if rule.Pair == nil {
		t.Fatalf("expected pair rule, got %#v", rule)
	}
	if rule.Conditions.GroupResource != "deployments.apps" {
		t.Fatalf("unexpected groupResource: %s", rule.Conditions.GroupResource)
	}
	if rule.Pair.Path != "/spec/template/spec/containers/0/image" {
		t.Fatalf("unexpected path: %s", rule.Pair.Path)
	}
	if rule.Pair.SourceValue != "10.11.11.1:5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0" {
		t.Fatalf("unexpected source image: %s", rule.Pair.SourceValue)
	}
	if rule.Pair.TargetValue != "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/bcs-bkcmdb-synchronizer:v1.31.0" {
		t.Fatalf("unexpected target image: %s", rule.Pair.TargetValue)
	}
}

func TestCompileDynamicImageRewriteRules_PreservesDigestSuffix(t *testing.T) {
	t.Parallel()

	source := newDynamicImageRewriteFakeClient(t,
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "digest-app", Namespace: "blueking"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "app",
							Image: "10.11.11.1:5000/blueking/app@sha256:abcdef",
						}},
					},
				},
			},
		},
	)
	instance := dynamicImageRewriteInstance("cluster-a", "cluster-b", []disasterv1.BulkModifierAction{dynamicImageRewriteAction(
		"rewrite-digest",
		"10.11.11.1:5000/",
		"registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/",
		disasterv1.ImageRewriteUnmatchedPolicyKeep,
	)})
	instance.Spec.RestorePolicy.BulkModifierActions[0].ImageRewrite.DigestPolicy = disasterv1.ImageRewriteDigestPolicyPreserve

	rules, _, err := (&DynamicImageRewriteCompiler{}).CompileDynamicImageRewriteRules(
		context.Background(),
		instance,
		source,
		disasterv1.RestoreModifierApplyResourceSync,
		WithDynamicImageRewriteBaseline("cluster-a", "cluster-b"),
	)
	if err != nil {
		t.Fatalf("CompileDynamicImageRewriteRules returned error: %v", err)
	}
	if len(rules) != 1 || rules[0].Pair == nil {
		t.Fatalf("expected one pair rule, got %#v", rules)
	}
	if got := rules[0].Pair.TargetValue; got != "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/app@sha256:abcdef" {
		t.Fatalf("unexpected digest target: %s", got)
	}
}

func TestCompileDynamicImageRewriteRules_CoversMultipleDeploymentInitContainers(t *testing.T) {
	t.Parallel()

	source := newDynamicImageRewriteFakeClient(t,
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "bk-apigateway", Namespace: "blueking"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "api",
							Image: "10.134.81.9:5000/blueking/bk-apigateway:v1",
						}},
						InitContainers: []corev1.Container{
							{
								Name:  "wait-storages",
								Image: "10.134.81.9:5000/groundnuty/k8s-wait-for:v1.5.1",
							},
							{
								Name:  "bk-apigateway-operator",
								Image: "10.134.81.9:5000/groundnuty/k8s-wait-for:v1.5.1",
							},
						},
					},
				},
			},
		},
	)
	instance := dynamicImageRewriteInstance("cluster-a", "cluster-b", []disasterv1.BulkModifierAction{dynamicImageRewriteAction(
		"rewrite-primary-registry",
		"10.134.81.9:5000/",
		"registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/",
		disasterv1.ImageRewriteUnmatchedPolicyKeep,
	)})

	rules, summary, err := (&DynamicImageRewriteCompiler{}).CompileDynamicImageRewriteRules(
		context.Background(),
		instance,
		source,
		disasterv1.RestoreModifierApplyResourceSync,
		WithDynamicImageRewriteBaseline("cluster-a", "cluster-b"),
	)
	if err != nil {
		t.Fatalf("CompileDynamicImageRewriteRules returned error: %v", err)
	}
	if summary.GeneratedRuleCount != 3 || summary.MatchedImageCount != 3 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	paths := dynamicImageRewriteRuleTargetsByPath(rules)
	if got := paths["/spec/template/spec/initContainers/0/image"]; got != "registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/groundnuty/k8s-wait-for:v1.5.1" {
		t.Fatalf("unexpected first initContainer target: %s", got)
	}
	if got := paths["/spec/template/spec/initContainers/1/image"]; got != "registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/groundnuty/k8s-wait-for:v1.5.1" {
		t.Fatalf("unexpected second initContainer target: %s", got)
	}
}

func TestCompileDynamicImageRewriteRules_CoversMultipleJobInitContainers(t *testing.T) {
	t.Parallel()

	source := newDynamicImageRewriteFakeClient(t,
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "bk-apigateway-wait-storages", Namespace: "blueking"},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyNever,
						Containers: []corev1.Container{{
							Name:  "job",
							Image: "10.134.81.9:5000/blueking/job-runner:v1",
						}},
						InitContainers: []corev1.Container{
							{
								Name:  "wait-storages",
								Image: "10.134.81.9:5000/groundnuty/k8s-wait-for:v1.5.1",
							},
							{
								Name:  "bk-apigateway-operator",
								Image: "10.134.81.9:5000/groundnuty/k8s-wait-for:v1.5.1",
							},
						},
					},
				},
			},
		},
	)
	instance := dynamicImageRewriteInstance("cluster-a", "cluster-b", []disasterv1.BulkModifierAction{dynamicImageRewriteAction(
		"rewrite-primary-registry",
		"10.134.81.9:5000/",
		"registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/",
		disasterv1.ImageRewriteUnmatchedPolicyKeep,
	)})

	rules, summary, err := (&DynamicImageRewriteCompiler{}).CompileDynamicImageRewriteRules(
		context.Background(),
		instance,
		source,
		disasterv1.RestoreModifierApplyResourceSync,
		WithDynamicImageRewriteBaseline("cluster-a", "cluster-b"),
	)
	if err != nil {
		t.Fatalf("CompileDynamicImageRewriteRules returned error: %v", err)
	}
	if summary.GeneratedRuleCount != 3 || summary.MatchedImageCount != 3 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	paths := dynamicImageRewriteRuleTargetsByPath(rules)
	if got := paths["/spec/template/spec/initContainers/0/image"]; got != "registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/groundnuty/k8s-wait-for:v1.5.1" {
		t.Fatalf("unexpected first initContainer target: %s", got)
	}
	if got := paths["/spec/template/spec/initContainers/1/image"]; got != "registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/groundnuty/k8s-wait-for:v1.5.1" {
		t.Fatalf("unexpected second initContainer target: %s", got)
	}
}

func TestCompileDynamicImageRewriteRules_CoversReplicaSetInitContainers(t *testing.T) {
	t.Parallel()

	source := newDynamicImageRewriteFakeClient(t,
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Name: "bk-apigateway-abc123", Namespace: "blueking"},
			Spec: appsv1.ReplicaSetSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "api",
							Image: "10.134.81.9:5000/blueking/bk-apigateway:v1",
						}},
						InitContainers: []corev1.Container{
							{
								Name:  "wait-storages",
								Image: "10.134.81.9:5000/groundnuty/k8s-wait-for:v1.5.1",
							},
							{
								Name:  "bk-apigateway-operator",
								Image: "10.134.81.9:5000/groundnuty/k8s-wait-for:v1.5.1",
							},
						},
					},
				},
			},
		},
	)
	instance := dynamicImageRewriteInstance("cluster-a", "cluster-b", []disasterv1.BulkModifierAction{dynamicImageRewriteAction(
		"rewrite-primary-registry",
		"10.134.81.9:5000/",
		"registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/",
		disasterv1.ImageRewriteUnmatchedPolicyKeep,
	)})

	rules, summary, err := (&DynamicImageRewriteCompiler{}).CompileDynamicImageRewriteRules(
		context.Background(),
		instance,
		source,
		disasterv1.RestoreModifierApplyResourceSync,
		WithDynamicImageRewriteBaseline("cluster-a", "cluster-b"),
	)
	if err != nil {
		t.Fatalf("CompileDynamicImageRewriteRules returned error: %v", err)
	}
	if summary.GeneratedRuleCount != 3 || summary.MatchedImageCount != 3 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	rule := dynamicImageRewriteRuleByPath(rules, "/spec/template/spec/initContainers/1/image")
	if rule == nil {
		t.Fatalf("expected second initContainer rule, got %#v", rules)
	}
	if rule.Conditions.GroupResource != "replicasets.apps" {
		t.Fatalf("unexpected groupResource: %s", rule.Conditions.GroupResource)
	}
	if rule.Pair == nil || rule.Pair.TargetValue != "registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/groundnuty/k8s-wait-for:v1.5.1" {
		t.Fatalf("unexpected second initContainer target rule: %#v", rule)
	}
}

func TestCompileDynamicImageRewriteRules_CoversReplicationControllerInitContainers(t *testing.T) {
	t.Parallel()

	source := newDynamicImageRewriteFakeClient(t,
		&corev1.ReplicationController{
			ObjectMeta: metav1.ObjectMeta{Name: "legacy-api", Namespace: "blueking"},
			Spec: corev1.ReplicationControllerSpec{
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "api",
							Image: "10.134.81.9:5000/blueking/legacy-api:v1",
						}},
						InitContainers: []corev1.Container{
							{
								Name:  "wait-storages",
								Image: "10.134.81.9:5000/groundnuty/k8s-wait-for:v1.5.1",
							},
						},
					},
				},
			},
		},
	)
	instance := dynamicImageRewriteInstance("cluster-a", "cluster-b", []disasterv1.BulkModifierAction{dynamicImageRewriteAction(
		"rewrite-primary-registry",
		"10.134.81.9:5000/",
		"registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/",
		disasterv1.ImageRewriteUnmatchedPolicyKeep,
	)})

	rules, summary, err := (&DynamicImageRewriteCompiler{}).CompileDynamicImageRewriteRules(
		context.Background(),
		instance,
		source,
		disasterv1.RestoreModifierApplyResourceSync,
		WithDynamicImageRewriteBaseline("cluster-a", "cluster-b"),
	)
	if err != nil {
		t.Fatalf("CompileDynamicImageRewriteRules returned error: %v", err)
	}
	if summary.GeneratedRuleCount != 2 || summary.MatchedImageCount != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	rule := dynamicImageRewriteRuleByPath(rules, "/spec/template/spec/initContainers/0/image")
	if rule == nil {
		t.Fatalf("expected initContainer rule, got %#v", rules)
	}
	if rule.Conditions.GroupResource != "replicationcontrollers" {
		t.Fatalf("unexpected groupResource: %s", rule.Conditions.GroupResource)
	}
	if rule.Pair == nil || rule.Pair.TargetValue != "registry-tke.szmacloud.csg:30088/dr_images/10.134.81.9_5000/groundnuty/k8s-wait-for:v1.5.1" {
		t.Fatalf("unexpected initContainer target rule: %#v", rule)
	}
}

func TestCompileDynamicImageRewriteRules_UsesLongestPrefix(t *testing.T) {
	t.Parallel()

	source := newDynamicImageRewriteFakeClient(t,
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "blueking-app", Namespace: "blueking"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "app",
							Image: "10.11.11.1:5000/blueking/app:v2",
						}},
					},
				},
			},
		},
	)
	instance := dynamicImageRewriteInstance("cluster-a", "cluster-b", []disasterv1.BulkModifierAction{
		dynamicImageRewriteAction("generic", "10.11.11.1:5000/", "registry.example.com/generic/", disasterv1.ImageRewriteUnmatchedPolicyKeep),
		dynamicImageRewriteAction("blueking", "10.11.11.1:5000/blueking/", "registry.example.com/blueking/", disasterv1.ImageRewriteUnmatchedPolicyKeep),
	})

	rules, _, err := (&DynamicImageRewriteCompiler{}).CompileDynamicImageRewriteRules(
		context.Background(),
		instance,
		source,
		disasterv1.RestoreModifierApplyResourceSync,
		WithDynamicImageRewriteBaseline("cluster-a", "cluster-b"),
	)
	if err != nil {
		t.Fatalf("CompileDynamicImageRewriteRules returned error: %v", err)
	}
	if len(rules) != 1 || rules[0].Pair == nil {
		t.Fatalf("expected one pair rule, got %#v", rules)
	}
	if got := rules[0].Pair.TargetValue; got != "registry.example.com/blueking/app:v2" {
		t.Fatalf("expected longest-prefix target, got %s", got)
	}
}

func TestCompileDynamicImageRewriteRules_SameLongestPrefixConflictFails(t *testing.T) {
	t.Parallel()

	source := newDynamicImageRewriteFakeClient(t,
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "conflict-app", Namespace: "blueking"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "app",
							Image: "10.11.11.1:5000/blueking/app:v2",
						}},
					},
				},
			},
		},
	)
	instance := dynamicImageRewriteInstance("cluster-a", "cluster-b", []disasterv1.BulkModifierAction{
		dynamicImageRewriteAction("target-a", "10.11.11.1:5000/", "registry-a.example.com/dr/", disasterv1.ImageRewriteUnmatchedPolicyKeep),
		dynamicImageRewriteAction("target-b", "10.11.11.1:5000/", "registry-b.example.com/dr/", disasterv1.ImageRewriteUnmatchedPolicyKeep),
	})

	_, summary, err := (&DynamicImageRewriteCompiler{}).CompileDynamicImageRewriteRules(
		context.Background(),
		instance,
		source,
		disasterv1.RestoreModifierApplyResourceSync,
		WithDynamicImageRewriteBaseline("cluster-a", "cluster-b"),
	)
	if err == nil {
		t.Fatalf("expected conflict failure")
	}
	if !strings.Contains(err.Error(), ModifierErrorRuleConflict) {
		t.Fatalf("expected conflict error code, got %v", err)
	}
	if !strings.Contains(err.Error(), "target-a") || !strings.Contains(err.Error(), "target-b") {
		t.Fatalf("expected action IDs in conflict error, got %v", err)
	}
	if summary.ConflictCount != 1 {
		t.Fatalf("expected one conflict, got %+v", summary)
	}
}

func TestCompileDynamicImageRewriteRules_ReverseFlowMatchesTargetPrefix(t *testing.T) {
	t.Parallel()

	source := newDynamicImageRewriteFakeClient(t,
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "reverse-app", Namespace: "blueking"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "app",
							Image: "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/app:v3",
						}},
					},
				},
			},
		},
	)
	instance := dynamicImageRewriteInstance("cluster-b", "cluster-a", []disasterv1.BulkModifierAction{dynamicImageRewriteAction(
		"reverse",
		"10.11.11.1:5000/",
		"registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/",
		disasterv1.ImageRewriteUnmatchedPolicyKeep,
	)})

	rules, _, err := (&DynamicImageRewriteCompiler{}).CompileDynamicImageRewriteRules(
		context.Background(),
		instance,
		source,
		disasterv1.RestoreModifierApplyResourceSync,
		WithDynamicImageRewriteBaseline("cluster-a", "cluster-b"),
	)
	if err != nil {
		t.Fatalf("CompileDynamicImageRewriteRules returned error: %v", err)
	}
	if len(rules) != 1 || rules[0].Pair == nil {
		t.Fatalf("expected one pair rule, got %#v", rules)
	}
	if got := rules[0].Pair.SourceValue; got != "10.11.11.1:5000/blueking/app:v3" {
		t.Fatalf("unexpected reverse source value: %s", got)
	}
	if got := rules[0].Pair.TargetValue; got != "registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/blueking/app:v3" {
		t.Fatalf("unexpected reverse target value: %s", got)
	}
}

func TestCompileDynamicImageRewriteRules_PodStatusImagesDoNotGenerateRules(t *testing.T) {
	t.Parallel()

	source := newDynamicImageRewriteFakeClient(t,
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "demo-pod", Namespace: "blueking"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "app",
					Image: "10.11.11.1:5000/blueking/app:v1",
				}},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					Name:  "app",
					Image: "10.11.11.1:5000/blueking/app-runtime:v1",
				}},
			},
		},
	)
	instance := dynamicImageRewriteInstance("cluster-a", "cluster-b", []disasterv1.BulkModifierAction{dynamicImageRewriteAction(
		"rewrite-pod",
		"10.11.11.1:5000/",
		"registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/",
		disasterv1.ImageRewriteUnmatchedPolicyKeep,
	)})

	rules, _, err := (&DynamicImageRewriteCompiler{}).CompileDynamicImageRewriteRules(
		context.Background(),
		instance,
		source,
		disasterv1.RestoreModifierApplyResourceSync,
		WithDynamicImageRewriteBaseline("cluster-a", "cluster-b"),
	)
	if err != nil {
		t.Fatalf("CompileDynamicImageRewriteRules returned error: %v", err)
	}
	if len(rules) != 1 || rules[0].Pair == nil {
		t.Fatalf("expected only spec image rule, got %#v", rules)
	}
	if strings.Contains(rules[0].Pair.Path, "/status") {
		t.Fatalf("status path must not be generated: %s", rules[0].Pair.Path)
	}
	if strings.Contains(rules[0].Pair.TargetValue, "runtime") {
		t.Fatalf("status image must not be used: %s", rules[0].Pair.TargetValue)
	}
}

func TestCompileDynamicImageRewriteRules_UnmatchedFailReturnsDetails(t *testing.T) {
	t.Parallel()

	source := newDynamicImageRewriteFakeClient(t,
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "foreign-app", Namespace: "blueking"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "app",
							Image: "docker.io/library/nginx:1.25",
						}},
					},
				},
			},
		},
	)
	instance := dynamicImageRewriteInstance("cluster-a", "cluster-b", []disasterv1.BulkModifierAction{dynamicImageRewriteAction(
		"strict",
		"10.11.11.1:5000/",
		"registry-test.xxx.xxx.com:30088/dr_images/10_11_11_1_5000/",
		disasterv1.ImageRewriteUnmatchedPolicyFail,
	)})

	_, summary, err := (&DynamicImageRewriteCompiler{}).CompileDynamicImageRewriteRules(
		context.Background(),
		instance,
		source,
		disasterv1.RestoreModifierApplyResourceSync,
		WithDynamicImageRewriteBaseline("cluster-a", "cluster-b"),
	)
	if err == nil {
		t.Fatalf("expected unmatched failure")
	}
	if !strings.Contains(err.Error(), "foreign-app") || !strings.Contains(err.Error(), "docker.io/library/nginx:1.25") {
		t.Fatalf("expected unmatched details, got %v", err)
	}
	if summary.UnmatchedImageCount != 1 {
		t.Fatalf("expected one unmatched image, got %+v", summary)
	}
}

func newDynamicImageRewriteFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add apps scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add batch scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func dynamicImageRewriteRuleTargetsByPath(rules []disasterv1.RestoreModifierRule) map[string]string {
	out := make(map[string]string, len(rules))
	for _, rule := range rules {
		if rule.Pair == nil {
			continue
		}
		out[rule.Pair.Path] = rule.Pair.TargetValue
	}
	return out
}

func dynamicImageRewriteRuleByPath(rules []disasterv1.RestoreModifierRule, path string) *disasterv1.RestoreModifierRule {
	for idx := range rules {
		if rules[idx].Pair != nil && rules[idx].Pair.Path == path {
			return &rules[idx]
		}
	}
	return nil
}

func dynamicImageRewriteInstance(primary, secondary string, actions []disasterv1.BulkModifierAction) *disasterv1.DisasterInstance {
	return &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"blueking"},
			RestorePolicy: &disasterv1.RestorePolicy{
				UseUnifiedDirectionResolver: boolPtr(true),
				BulkModifierActions:         actions,
			},
		},
		Status: disasterv1.DisasterInstanceStatus{
			PrimaryCluster:   primary,
			SecondaryCluster: secondary,
		},
	}
}

func dynamicImageRewriteAction(
	id string,
	sourcePrefix string,
	targetPrefix string,
	unmatched disasterv1.ImageRewriteUnmatchedPolicy,
) disasterv1.BulkModifierAction {
	return disasterv1.BulkModifierAction{
		ID:              id,
		Action:          disasterv1.BulkModifierActionRewriteImage,
		Enabled:         boolPtr(true),
		ApplyTo:         []disasterv1.RestoreModifierApplyTarget{disasterv1.RestoreModifierApplyResourceSync},
		DirectionPolicy: disasterv1.RestoreModifierDirectionPolicyAuto,
		ImageRewrite: &disasterv1.DynamicImageRewriteConfig{
			SourcePrefix:    sourcePrefix,
			TargetPrefix:    targetPrefix,
			UnmatchedPolicy: unmatched,
		},
	}
}
