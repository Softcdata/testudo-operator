package disasteroperation

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestShouldDrillCleanupPVCVolumeName(t *testing.T) {
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app-ns"},
		},
	}

	cases := []struct {
		name            string
		instance        *disasterv1.DisasterInstance
		namespaceMapper map[string]string
		want            bool
	}{
		{
			name:            "nil instance",
			instance:        nil,
			namespaceMapper: map[string]string{"app-ns": "drill-ns"},
			want:            false,
		},
		{
			name:            "empty mapping",
			instance:        instance,
			namespaceMapper: nil,
			want:            false,
		},
		{
			name:            "same namespace mapping",
			instance:        instance,
			namespaceMapper: map[string]string{"app-ns": "app-ns"},
			want:            false,
		},
		{
			name:            "new target namespace mapping",
			instance:        instance,
			namespaceMapper: map[string]string{"app-ns": "drill-ns"},
			want:            true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldDrillCleanupPVCVolumeName(tc.instance, tc.namespaceMapper)
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestDrillPVCVolumeCleanupNamespaces(t *testing.T) {
	got := drillPVCVolumeCleanupNamespaces(
		[]string{"app-ns", " db-ns "},
		map[string]string{
			"app-ns": "drill-app-ns",
			"db-ns":  "drill-db-ns",
			"other":  "drill-app-ns", // duplicate destination should be deduped
		},
	)

	wantSet := map[string]struct{}{
		"app-ns":       {},
		"db-ns":        {},
		"drill-app-ns": {},
		"drill-db-ns":  {},
	}
	if len(got) != len(wantSet) {
		t.Fatalf("unexpected namespace count: got=%d want=%d (%#v)", len(got), len(wantSet), got)
	}
	for _, ns := range got {
		if _, ok := wantSet[ns]; !ok {
			t.Fatalf("unexpected namespace in result: %s (%#v)", ns, got)
		}
		delete(wantSet, ns)
	}
	if len(wantSet) != 0 {
		t.Fatalf("missing namespaces in result: %#v", wantSet)
	}
}

func TestBuildDrillPVCVolumeNameCleanupRule(t *testing.T) {
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app-ns"},
		},
	}

	rule, ok := buildDrillPVCVolumeNameCleanupRule(instance, map[string]string{"app-ns": "drill-app-ns"})
	if !ok {
		t.Fatalf("expected cleanup rule to be enabled")
	}
	if rule.Conditions.GroupResource != "persistentvolumeclaims" {
		t.Fatalf("expected pvc groupResource, got %s", rule.Conditions.GroupResource)
	}
	if len(rule.Patches) != 1 || rule.Patches[0].Operation != "add" || rule.Patches[0].Path != "/spec/volumeName" || rule.Patches[0].Value != "" {
		t.Fatalf("unexpected patches: %#v", rule.Patches)
	}
	contains := map[string]bool{}
	for _, ns := range rule.Conditions.Namespaces {
		contains[ns] = true
	}
	if !contains["app-ns"] || !contains["drill-app-ns"] {
		t.Fatalf("expected rule namespaces include source and mapped namespace, got %#v", rule.Conditions.Namespaces)
	}

	if _, ok := buildDrillPVCVolumeNameCleanupRule(instance, map[string]string{"app-ns": "app-ns"}); ok {
		t.Fatalf("did not expect cleanup rule for same-namespace mapping")
	}
}

func TestCleanupTrafficlessPodsDeletesPendingPodsAfterRestoreSucceeded(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := disasterv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	trafficlessPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "trafficless-pending",
			Namespace: "drill-ns",
			Labels: map[string]string{
				"trafficless": "true",
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Name:  "init-etcd",
				Image: "10.134.81.9:5000/blueking/bcs-busybox:v1.21.4",
			}},
			Containers: []corev1.Container{{
				Name:  "app",
				Image: "registry.local/rongzai/busybox:1.36",
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
	targetClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(trafficlessPod).
		Build()

	controlClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(&disasterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "target-cluster"},
			Spec: disasterv1.ClusterSpec{
				KubeConfig: []byte(testKubeConfigForDisasterOperationCleanup),
			},
		}).
		Build()

	r := &DisasterOperationReconciler{
		Client: controlClient,
		Scheme: s,
		ClientFactory: func(_ *rest.Config, _ ctrlclient.Options) (ctrlclient.Client, error) {
			return targetClient, nil
		},
	}
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app-ns"},
		},
	}

	done, err := r.cleanupTrafficlessPods(ctx, logr.Discard(), instance, "target-cluster", map[string]string{"app-ns": "drill-ns"})
	if err != nil {
		t.Fatalf("cleanupTrafficlessPods returned error: %v", err)
	}
	if done {
		t.Fatalf("expected first cleanup pass to wait for deletion confirmation")
	}

	deleted := &corev1.Pod{}
	err = targetClient.Get(ctx, types.NamespacedName{Namespace: "drill-ns", Name: "trafficless-pending"}, deleted)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected pending trafficless pod to be deleted, got err=%v pod=%#v", err, deleted)
	}

	done, err = r.cleanupTrafficlessPods(ctx, logr.Discard(), instance, "target-cluster", map[string]string{"app-ns": "drill-ns"})
	if err != nil {
		t.Fatalf("second cleanupTrafficlessPods returned error: %v", err)
	}
	if !done {
		t.Fatalf("expected cleanup to complete after trafficless pod disappeared")
	}
}

const testKubeConfigForDisasterOperationCleanup = `
apiVersion: v1
clusters:
- cluster:
    server: https://127.0.0.1
  name: test
contexts:
- context:
    cluster: test
    user: test
  name: test
current-context: test
kind: Config
preferences: {}
users:
- name: test
  user:
    token: test
`
