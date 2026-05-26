package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func buildVeleroCRDForCompatibilityTest(name string, served bool, storage bool) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "velero.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: strings.Split(name, ".")[0],
				Kind:   "Fake",
			},
			Scope: "Namespaced",
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    veleroRequiredCRDVersion,
					Served:  served,
					Storage: storage,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
					},
				},
			},
		},
	}
}

func buildCompatibleVeleroCRDObjects() []client.Object {
	objs := make([]client.Object, 0, len(requiredVeleroCRDNames))
	for _, name := range requiredVeleroCRDNames {
		objs = append(objs, buildVeleroCRDForCompatibilityTest(name, true, true))
	}
	return objs
}

func TestCheckVeleroVersionCompatibility(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		version   string
		wantEmpty bool
	}{
		{name: "supported exact", version: "v1.17.0", wantEmpty: true},
		{name: "supported patch", version: "1.17.9+build.1", wantEmpty: true},
		{name: "below range", version: "v1.16.9", wantEmpty: false},
		{name: "invalid format", version: "not-a-version", wantEmpty: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := checkVeleroVersionCompatibility(tc.version)
			if tc.wantEmpty && msg != "" {
				t.Fatalf("expected compatible version, got error message: %s", msg)
			}
			if !tc.wantEmpty && msg == "" {
				t.Fatalf("expected incompatible version error for %q", tc.version)
			}
		})
	}
}

func TestCheckVeleroCRDCompatibility(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := buildCleanupTestScheme(t)

	t.Run("compatible crds should pass", func(t *testing.T) {
		t.Parallel()
		cli := fakeclient.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(buildCompatibleVeleroCRDObjects()...).
			Build()
		reason, message := checkVeleroCRDCompatibility(ctx, cli)
		if reason != "" || message != "" {
			t.Fatalf("expected compatible CRDs, got reason=%s message=%s", reason, message)
		}
	})

	t.Run("missing required crd should fail with incompatible reason", func(t *testing.T) {
		t.Parallel()
		// omit one required CRD to simulate incompatible cluster
		objs := buildCompatibleVeleroCRDObjects()
		objs = objs[:len(objs)-1]
		cli := fakeclient.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objs...).
			Build()
		reason, message := checkVeleroCRDCompatibility(ctx, cli)
		if reason != clusterReasonVeleroCRDVersionIncompatible {
			t.Fatalf("unexpected reason: %s", reason)
		}
		if !strings.Contains(message, "not found") {
			t.Fatalf("unexpected message: %s", message)
		}
	})

	t.Run("missing served/storage v1 should fail with incompatible reason", func(t *testing.T) {
		t.Parallel()
		objs := buildCompatibleVeleroCRDObjects()
		objs[0] = buildVeleroCRDForCompatibilityTest(requiredVeleroCRDNames[0], false, false)
		cli := fakeclient.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objs...).
			Build()
		reason, message := checkVeleroCRDCompatibility(ctx, cli)
		if reason != clusterReasonVeleroCRDVersionIncompatible {
			t.Fatalf("unexpected reason: %s", reason)
		}
		if !strings.Contains(message, "requires served version") {
			t.Fatalf("unexpected message: %s", message)
		}
	})

	t.Run("client get failure should fail with check-failed reason", func(t *testing.T) {
		t.Parallel()
		mockClient := &MockClient{
			MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				return fmt.Errorf("forbidden")
			},
		}
		reason, message := checkVeleroCRDCompatibility(ctx, mockClient)
		if reason != clusterReasonVeleroCRDCheckFailed {
			t.Fatalf("unexpected reason: %s", reason)
		}
		if !strings.Contains(message, "failed to validate velero crd compatibility") {
			t.Fatalf("unexpected message: %s", message)
		}
	})
}

func TestCheckVeleroCompatibility(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := buildCleanupTestScheme(t)
	r := &ClusterReconciler{}

	t.Run("version incompatible should return version reason", func(t *testing.T) {
		t.Parallel()
		cli := fakeclient.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(buildCompatibleVeleroCRDObjects()...).
			Build()
		reason, message := r.checkVeleroCompatibility(ctx, cli, "v1.16.0")
		if reason != clusterReasonVeleroVersionIncompatible {
			t.Fatalf("unexpected reason: %s", reason)
		}
		if !strings.Contains(message, "velero version incompatible") {
			t.Fatalf("unexpected message: %s", message)
		}
	})

	t.Run("crd incompatible should return crd reason", func(t *testing.T) {
		t.Parallel()
		objs := buildCompatibleVeleroCRDObjects()
		objs = objs[:len(objs)-1]
		cli := fakeclient.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objs...).
			Build()
		reason, message := r.checkVeleroCompatibility(ctx, cli, "v1.17.0")
		if reason != clusterReasonVeleroCRDVersionIncompatible {
			t.Fatalf("unexpected reason: %s", reason)
		}
		if !strings.Contains(message, "velero crd incompatible") {
			t.Fatalf("unexpected message: %s", message)
		}
	})

	t.Run("compatible version and crds should pass", func(t *testing.T) {
		t.Parallel()
		cli := fakeclient.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(buildCompatibleVeleroCRDObjects()...).
			Build()
		reason, message := r.checkVeleroCompatibility(ctx, cli, "v1.17.0")
		if reason != "" || message != "" {
			t.Fatalf("expected compatibility check to pass, got reason=%s message=%s", reason, message)
		}
	})
}

func TestDiagnoseVeleroStatusPendingIgnoresNonRuntimePods(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := buildCleanupTestScheme(t)
	cli := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "velero", Namespace: VeleroNamespace},
				Status: appsv1.DeploymentStatus{
					ReadyReplicas:       1,
					AvailableReplicas:   1,
					UnavailableReplicas: 0,
				},
			},
			&appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: VeleroNamespace},
				Status: appsv1.DaemonSetStatus{
					DesiredNumberScheduled: 1,
					NumberReady:            1,
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "repo-maintain-job-failed", Namespace: VeleroNamespace},
				Status: corev1.PodStatus{
					Phase:  corev1.PodFailed,
					Reason: "Error",
				},
			},
		).
		Build()

	reason, message := diagnoseVeleroStatusPending(ctx, cli, 7)
	if reason != clusterReasonVeleroStatusSyncPending {
		t.Fatalf("expected status-sync pending reason, got %s (%s)", reason, message)
	}
	if strings.Contains(message, "repo-maintain-job-failed") {
		t.Fatalf("expected non-runtime pod to be ignored, got message %q", message)
	}
}

func TestUpdateClusterStatusWithRetry_RetriesConflict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := buildCleanupTestScheme(t)
	clusterResource := schema.GroupResource{Group: "testudo.softcdata.com", Resource: "clusters"}
	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-status-retry"},
	}

	baseClient := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&disasterv1.Cluster{}).
		WithObjects(cluster).
		Build()

	statusUpdates := 0
	reconciler := &ClusterReconciler{
		Client: &MockClient{
			Client: baseClient,
			MockStatus: func() client.StatusWriter {
				return &MockStatusWriter{
					StatusWriter: baseClient.Status(),
					MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
						statusUpdates++
						if statusUpdates == 1 {
							return apierrors.NewConflict(clusterResource, obj.GetName(), fmt.Errorf("object has been modified"))
						}
						return baseClient.Status().Update(ctx, obj, opts...)
					},
				}
			},
		},
	}

	cluster.Status.Status = disasterv1.ClusterStatusReady
	cluster.Status.Reason = "Recovered"
	cluster.Status.Message = "status persisted after retry"

	if err := reconciler.updateClusterStatusWithRetry(ctx, cluster); err != nil {
		t.Fatalf("updateClusterStatusWithRetry failed: %v", err)
	}
	if statusUpdates != 2 {
		t.Fatalf("expected two status update attempts, got %d", statusUpdates)
	}

	updated := &disasterv1.Cluster{}
	if err := baseClient.Get(ctx, client.ObjectKeyFromObject(cluster), updated); err != nil {
		t.Fatalf("failed to read updated cluster: %v", err)
	}
	if updated.Status.Status != disasterv1.ClusterStatusReady {
		t.Fatalf("expected ready status after retry, got %s", updated.Status.Status)
	}
	if updated.Status.Reason != "Recovered" {
		t.Fatalf("expected reason to persist, got %s", updated.Status.Reason)
	}
	if updated.Status.Message != "status persisted after retry" {
		t.Fatalf("expected message to persist, got %s", updated.Status.Message)
	}
}

func TestUpdateClusterLabelsWithRetry_RetriesConflict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := buildCleanupTestScheme(t)
	clusterResource := schema.GroupResource{Group: "testudo.softcdata.com", Resource: "clusters"}
	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-label-retry"},
	}

	baseClient := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		Build()

	labelUpdates := 0
	reconciler := &ClusterReconciler{
		Client: &MockClient{
			Client: baseClient,
			MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
				labelUpdates++
				if labelUpdates == 1 {
					return apierrors.NewConflict(clusterResource, obj.GetName(), fmt.Errorf("object has been modified"))
				}
				return baseClient.Update(ctx, obj, opts...)
			},
		},
	}

	ownedLabels := map[string]string{
		"testudo.softcdata.com/dependency-token": "abc123",
	}
	if err := reconciler.updateClusterLabelsWithRetry(ctx, cluster, ownedLabels); err != nil {
		t.Fatalf("updateClusterLabelsWithRetry failed: %v", err)
	}
	if labelUpdates != 2 {
		t.Fatalf("expected two label update attempts, got %d", labelUpdates)
	}

	updated := &disasterv1.Cluster{}
	if err := baseClient.Get(ctx, client.ObjectKeyFromObject(cluster), updated); err != nil {
		t.Fatalf("failed to read updated cluster: %v", err)
	}
	if got := updated.Labels["testudo.softcdata.com/dependency-token"]; got != "abc123" {
		t.Fatalf("expected label to persist after retry, got %q", got)
	}
	if got := cluster.Labels["testudo.softcdata.com/dependency-token"]; got != "abc123" {
		t.Fatalf("expected in-memory cluster label to stay in sync, got %q", got)
	}
}

func TestReportCreateClusterFailedForCompatibility(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := buildCleanupTestScheme(t)

	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cluster-compat-fail",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-30 * time.Second)),
			Annotations: map[string]string{
				"testudo.softcdata.com/trace-id": "trace-compat-fail",
				"testudo.softcdata.com/user":     "tester",
			},
		},
	}
	eventNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: helper.DefaultEventNamespace}}
	cli := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, eventNS).Build()
	r := &ClusterReconciler{Client: cli, Scheme: scheme}

	r.reportCreateClusterFailedForCompatibility(ctx, cluster, clusterReasonVeleroVersionIncompatible, "velero version incompatible")
	r.reportCreateClusterFailedForCompatibility(ctx, cluster, clusterReasonVeleroVersionIncompatible, "velero version incompatible")

	events := &corev1.EventList{}
	if err := cli.List(ctx, events, client.InNamespace(helper.DefaultEventNamespace)); err != nil {
		t.Fatalf("list events failed: %v", err)
	}
	finishedCount := 0
	for i := range events.Items {
		ev := events.Items[i]
		if ev.Reason == helper.EventReasonExecutionFinished && ev.Type == corev1.EventTypeWarning {
			finishedCount++
		}
	}
	if finishedCount != 1 {
		t.Fatalf("expected one warning execution-finished event, got=%d", finishedCount)
	}
	if cluster.Status.LastEventPhase != string(disasterv1.ClusterStatusNotReady) {
		t.Fatalf("expected LastEventPhase=NotReady, got=%s", cluster.Status.LastEventPhase)
	}
}
