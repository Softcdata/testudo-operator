package controller

import (
	"context"
	"fmt"
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newRefreshStatsRemoteClient(namespaces []string, runningWorkloadByNamespace map[string]map[string]int, namespaceStatsByNamespace map[string]map[string]int) client.Client {
	return &MockClient{
		MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
			switch typed := list.(type) {
			case *corev1.NamespaceList:
				typed.Items = make([]corev1.Namespace, 0, len(namespaces))
				for _, ns := range namespaces {
					typed.Items = append(typed.Items, corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
				}
				return nil
			case *appsv1.DeploymentList:
				typed.Items = nil
				for ns, byKind := range runningWorkloadByNamespace {
					for i := 0; i < byKind["Deployment"]; i++ {
						typed.Items = append(typed.Items, appsv1.Deployment{
							ObjectMeta: metav1.ObjectMeta{
								Name:      fmt.Sprintf("%s-deployment-%d", ns, i),
								Namespace: ns,
							},
							Status: appsv1.DeploymentStatus{
								ReadyReplicas:     1,
								AvailableReplicas: 1,
							},
						})
					}
				}
				return nil
			case *appsv1.StatefulSetList:
				typed.Items = nil
				for ns, byKind := range runningWorkloadByNamespace {
					for i := 0; i < byKind["StatefulSet"]; i++ {
						typed.Items = append(typed.Items, appsv1.StatefulSet{
							ObjectMeta: metav1.ObjectMeta{
								Name:      fmt.Sprintf("%s-statefulset-%d", ns, i),
								Namespace: ns,
							},
							Status: appsv1.StatefulSetStatus{
								ReadyReplicas: 1,
							},
						})
					}
				}
				return nil
			case *metav1.PartialObjectMetadataList:
				listOpts := &client.ListOptions{}
				for _, opt := range opts {
					opt.ApplyToList(listOpts)
				}
				ns := listOpts.Namespace
				kind := typed.GetObjectKind().GroupVersionKind().Kind
				if ns != "" {
					count := 0
					if byKind, ok := namespaceStatsByNamespace[ns]; ok {
						count += byKind[kind]
					}
					typed.Items = make([]metav1.PartialObjectMetadata, count)
					for i := 0; i < count; i++ {
						typed.Items[i] = metav1.PartialObjectMetadata{
							ObjectMeta: metav1.ObjectMeta{
								Name:      fmt.Sprintf("%s-%s-%d", ns, kind, i),
								Namespace: ns,
							},
						}
					}
					return nil
				}
				typed.Items = nil
				for namespace, byKind := range namespaceStatsByNamespace {
					for i := 0; i < byKind[kind]; i++ {
						typed.Items = append(typed.Items, metav1.PartialObjectMetadata{
							ObjectMeta: metav1.ObjectMeta{
								Name:      fmt.Sprintf("%s-%s-%d", namespace, kind, i),
								Namespace: namespace,
							},
						})
					}
				}
				return nil
			default:
				return fmt.Errorf("unsupported list type %T", list)
			}
		},
	}
}

func TestCollectWorkloadNamespaceStatsCountsOnlyNamespacesWithWorkloads(t *testing.T) {
	cluster := &disasterv1.Cluster{}
	reconciler := &ClusterReconciler{}
	remoteCli := newRefreshStatsRemoteClient(
		[]string{"ns-a", "ns-b", "ns-c"},
		map[string]map[string]int{
			"ns-a": {"Deployment": 1},
			"ns-b": {"StatefulSet": 1},
		},
		map[string]map[string]int{
			"ns-a": {"Deployment": 1, "ConfigMap": 2, "Pod": 3},
			"ns-b": {"StatefulSet": 1, "Service": 2, "Event": 5},
			"ns-c": {"Deployment": 1, "Secret": 4},
		},
	)

	if err := reconciler.collectWorkloadNamespaceStats(context.Background(), remoteCli, nil, cluster); err != nil {
		t.Fatalf("collectWorkloadNamespaceStats failed: %v", err)
	}

	if got, want := cluster.Status.WorkloadNamespaceCount, 2; got != want {
		t.Fatalf("unexpected workload namespace count, got=%d want=%d", got, want)
	}
	if got, want := cluster.Status.WorkloadTotalCount, 6; got != want {
		t.Fatalf("unexpected workload total count, got=%d want=%d", got, want)
	}
	if _, exists := cluster.Status.WorkloadNamespaceStats["ns-c"]; exists {
		t.Fatalf("expected namespace without workloads to be excluded from stats map")
	}
	if got := cluster.Status.WorkloadNamespaceStats["ns-a"]; got != 3 {
		t.Fatalf("unexpected ns-a resource count, got=%d", got)
	}
	if got := cluster.Status.WorkloadNamespaceStats["ns-b"]; got != 3 {
		t.Fatalf("unexpected ns-b resource count, got=%d", got)
	}
}

func TestCollectNamespaceStatsUsesBackupScope(t *testing.T) {
	cluster := &disasterv1.Cluster{}
	reconciler := &ClusterReconciler{}
	remoteCli := newRefreshStatsRemoteClient(
		[]string{"ns-a", "velero", "kube-system"},
		nil,
		map[string]map[string]int{
			"ns-a":        {"Deployment": 1, "Service": 2, "Pod": 4, "Event": 6},
			"velero":      {"Deployment": 3, "Service": 1},
			"kube-system": {"ConfigMap": 5},
		},
	)

	if err := reconciler.collectNamespaceStats(context.Background(), remoteCli, nil, cluster); err != nil {
		t.Fatalf("collectNamespaceStats failed: %v", err)
	}

	if got, want := cluster.Status.NamespaceCount, 1; got != want {
		t.Fatalf("unexpected namespace count, got=%d want=%d", got, want)
	}
	if got, want := cluster.Status.ResourceTotalCount, 3; got != want {
		t.Fatalf("unexpected resource total count, got=%d want=%d", got, want)
	}
	if got := cluster.Status.NamespaceStats["ns-a"]; got != 3 {
		t.Fatalf("unexpected ns-a resource count, got=%d", got)
	}
	if _, exists := cluster.Status.NamespaceStats["velero"]; exists {
		t.Fatalf("expected velero namespace to be excluded from namespace stats")
	}
	if _, exists := cluster.Status.NamespaceStats["kube-system"]; exists {
		t.Fatalf("expected kube-system namespace to be excluded from namespace stats")
	}
}

func TestProcessRefreshClusterStatsSignalSuccessClearsSignalAndUpdatesLabels(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := buildCleanupTestScheme(t)
	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-refresh-success",
			Labels: map[string]string{
				"custom": "keep",
			},
			Annotations: map[string]string{
				AnnotationRefreshClusterStats: string(ClusterStatsRefreshTypeWorkloadNamespaceStats),
			},
		},
		Status: disasterv1.ClusterStatus{
			NamespaceCount:     3,
			ResourceTotalCount: 10,
		},
	}
	cli := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&disasterv1.Cluster{}).
		WithObjects(cluster).
		Build()
	reconciler := &ClusterReconciler{Client: cli, Scheme: scheme}

	remoteCli := newRefreshStatsRemoteClient(
		[]string{"ns-a", "ns-b", "ns-c"},
		map[string]map[string]int{
			"ns-a": {"Deployment": 1},
			"ns-b": {"StatefulSet": 1},
		},
		map[string]map[string]int{
			"ns-a": {"Deployment": 1, "ConfigMap": 2, "Pod": 3},
			"ns-b": {"StatefulSet": 1, "Service": 2, "Event": 5},
			"ns-c": {"Deployment": 1, "Secret": 4},
		},
	)

	handled, err := reconciler.processRefreshClusterStatsSignal(ctx, remoteCli, nil, cluster)
	if err != nil {
		t.Fatalf("processRefreshClusterStatsSignal failed: %v", err)
	}
	if !handled {
		t.Fatalf("expected refresh signal to be handled")
	}

	updated := &disasterv1.Cluster{}
	if err := cli.Get(ctx, client.ObjectKey{Name: cluster.Name}, updated); err != nil {
		t.Fatalf("failed to reload cluster: %v", err)
	}
	if _, exists := updated.Annotations[AnnotationRefreshClusterStats]; exists {
		t.Fatalf("expected refresh signal annotation to be cleared")
	}
	if got := updated.Labels["custom"]; got != "keep" {
		t.Fatalf("expected custom label to remain unchanged, got=%q", got)
	}
	if got := updated.Labels[LabelClusterWorkloadNamespaceCount]; got != "2" {
		t.Fatalf("unexpected workload namespace label, got=%q", got)
	}
	if got := updated.Labels[LabelClusterWorkloadTotalCount]; got != "6" {
		t.Fatalf("unexpected workload total label, got=%q", got)
	}
	if got := updated.Status.WorkloadNamespaceCount; got != 2 {
		t.Fatalf("expected persisted workload namespace count, got=%d", got)
	}
	if got := updated.Status.WorkloadTotalCount; got != 6 {
		t.Fatalf("expected persisted workload total count, got=%d", got)
	}
	if got := updated.Status.WorkloadNamespaceStats["ns-a"]; got != 3 {
		t.Fatalf("expected persisted ns-a resource count, got=%d", got)
	}
	if cluster.Status.WorkloadNamespaceCount != 2 || cluster.Status.WorkloadTotalCount != 6 {
		t.Fatalf("expected in-memory cluster status to be updated, got count=%d total=%d", cluster.Status.WorkloadNamespaceCount, cluster.Status.WorkloadTotalCount)
	}
}

func TestProcessRefreshClusterStatsSignalInvalidTypeClearsSignal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := buildCleanupTestScheme(t)
	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-refresh-invalid",
			Annotations: map[string]string{
				AnnotationRefreshClusterStats: "unknown-type",
			},
		},
	}
	cli := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	reconciler := &ClusterReconciler{Client: cli, Scheme: scheme}

	handled, err := reconciler.processRefreshClusterStatsSignal(ctx, nil, nil, cluster)
	if err != nil {
		t.Fatalf("processRefreshClusterStatsSignal returned unexpected error: %v", err)
	}
	if !handled {
		t.Fatalf("expected invalid refresh signal to be handled")
	}

	updated := &disasterv1.Cluster{}
	if err := cli.Get(ctx, client.ObjectKey{Name: cluster.Name}, updated); err != nil {
		t.Fatalf("failed to reload cluster: %v", err)
	}
	if _, exists := updated.Annotations[AnnotationRefreshClusterStats]; exists {
		t.Fatalf("expected invalid refresh signal to be cleared")
	}
}

func TestProcessRefreshClusterStatsSignalTransientErrorKeepsSignal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := buildCleanupTestScheme(t)
	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-refresh-error",
			Annotations: map[string]string{
				AnnotationRefreshClusterStats: string(ClusterStatsRefreshTypeNamespaceStats),
			},
		},
	}
	cli := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	reconciler := &ClusterReconciler{Client: cli, Scheme: scheme}

	remoteCli := &MockClient{
		MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*corev1.NamespaceList); ok {
				return fmt.Errorf("list namespaces failed")
			}
			return nil
		},
	}

	handled, err := reconciler.processRefreshClusterStatsSignal(ctx, remoteCli, nil, cluster)
	if !handled {
		t.Fatalf("expected refresh signal to be handled")
	}
	if err == nil {
		t.Fatalf("expected transient error, got nil")
	}

	updated := &disasterv1.Cluster{}
	if err := cli.Get(ctx, client.ObjectKey{Name: cluster.Name}, updated); err != nil {
		t.Fatalf("failed to reload cluster: %v", err)
	}
	if got := updated.Annotations[AnnotationRefreshClusterStats]; got != string(ClusterStatsRefreshTypeNamespaceStats) {
		t.Fatalf("expected refresh signal to be retained, got=%q", got)
	}
}

func TestProcessRefreshClusterStatsSignalStatusUpdateFailureKeepsSignal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := buildCleanupTestScheme(t)
	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-refresh-status-error",
			Annotations: map[string]string{
				AnnotationRefreshClusterStats: string(ClusterStatsRefreshTypeWorkloadNamespaceStats),
			},
		},
	}

	baseClient := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&disasterv1.Cluster{}).
		WithObjects(cluster).
		Build()
	reconciler := &ClusterReconciler{
		Client: &MockClient{
			Client: baseClient,
			MockStatus: func() client.StatusWriter {
				return &MockStatusWriter{
					StatusWriter: baseClient.Status(),
					MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
						return fmt.Errorf("update status failed")
					},
				}
			},
		},
		Scheme: scheme,
	}

	remoteCli := newRefreshStatsRemoteClient(
		[]string{"ns-a"},
		map[string]map[string]int{
			"ns-a": {"Deployment": 1},
		},
		map[string]map[string]int{
			"ns-a": {"Deployment": 1, "ConfigMap": 2, "Pod": 3},
		},
	)

	handled, err := reconciler.processRefreshClusterStatsSignal(ctx, remoteCli, nil, cluster)
	if !handled {
		t.Fatalf("expected refresh signal to be handled")
	}
	if err == nil {
		t.Fatalf("expected status update error, got nil")
	}

	updated := &disasterv1.Cluster{}
	if err := baseClient.Get(ctx, client.ObjectKey{Name: cluster.Name}, updated); err != nil {
		t.Fatalf("failed to reload cluster: %v", err)
	}
	if _, exists := updated.Annotations[AnnotationRefreshClusterStats]; !exists {
		t.Fatalf("expected refresh signal to remain after status update failure")
	}
	if _, exists := updated.Labels[LabelClusterWorkloadTotalCount]; exists {
		t.Fatalf("expected labels to remain untouched when status update fails")
	}
}

func TestCalculateMetadataHashIgnoresRefreshSignalAndManagedWorkloadLabels(t *testing.T) {
	t.Parallel()

	r := &ClusterReconciler{}
	clusterA := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hash-cluster",
			Labels: map[string]string{
				"custom":                           "same",
				LabelClusterWorkloadNamespaceCount: "1",
				LabelClusterWorkloadTotalCount:     "3",
			},
			Annotations: map[string]string{
				"custom":                      "same",
				AnnotationRefreshClusterStats: string(ClusterStatsRefreshTypeNamespaceStats),
			},
		},
	}
	clusterB := clusterA.DeepCopy()
	clusterB.Labels[LabelClusterWorkloadNamespaceCount] = "99"
	clusterB.Labels[LabelClusterWorkloadTotalCount] = "100"
	clusterB.Annotations[AnnotationRefreshClusterStats] = string(ClusterStatsRefreshTypeAll)

	hashA := r.calculateMetadataHash(clusterA)
	hashB := r.calculateMetadataHash(clusterB)
	if hashA != hashB {
		t.Fatalf("expected refresh signal and managed workload labels to be excluded from metadata hash")
	}
}
