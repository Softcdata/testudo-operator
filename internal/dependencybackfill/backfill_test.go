package dependencybackfill

import (
	"context"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	metadata "github.com/softcdata/testudo-operator/pkg/metadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRunner_RunOnce_Idempotent(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := disasterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	cluster := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-a",
			UID:  types.UID("uid-cluster-a"),
		},
	}
	appBackup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ab-a",
			Namespace: "disaster-system",
			UID:       types.UID("uid-ab-a"),
		},
		Spec: disasterv1.AppBackupSpec{
			Cluster: "cluster-a",
		},
	}

	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, appBackup).
		Build()

	runner := NewRunner(cli, cli, logr.Discard())
	ctx := context.Background()

	if err := runner.RunOnce(ctx); err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	afterFirst := &disasterv1.AppBackup{}
	if err := cli.Get(ctx, client.ObjectKeyFromObject(appBackup), afterFirst); err != nil {
		t.Fatalf("get appbackup after first run: %v", err)
	}

	if afterFirst.Labels[metadata.LabelDependencyToken] == "" {
		t.Fatalf("expected dependency-token label")
	}

	clusterToken := metadata.BuildDependencyToken(string(cluster.UID))
	clusterEdgeKey := metadata.DependencyToLabelKey(clusterToken)
	if got := afterFirst.Labels[clusterEdgeKey]; got != "spec.cluster" {
		t.Fatalf("expected %s=spec.cluster, got %q", clusterEdgeKey, got)
	}

	firstLabels := make(map[string]string, len(afterFirst.Labels))
	for k, v := range afterFirst.Labels {
		firstLabels[k] = v
	}

	if err := runner.RunOnce(ctx); err != nil {
		t.Fatalf("second run failed: %v", err)
	}

	afterSecond := &disasterv1.AppBackup{}
	if err := cli.Get(ctx, client.ObjectKeyFromObject(appBackup), afterSecond); err != nil {
		t.Fatalf("get appbackup after second run: %v", err)
	}

	if !reflect.DeepEqual(firstLabels, afterSecond.Labels) {
		t.Fatalf("expected labels stable across rerun, first=%v second=%v", firstLabels, afterSecond.Labels)
	}
}
