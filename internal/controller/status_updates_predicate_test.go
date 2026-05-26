package controller

import (
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestIgnoreStatusUpdatesPredicate_Update(t *testing.T) {
	t.Parallel()

	predicate := IgnoreStatusUpdatesPredicate{}
	base := &disasterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "predicate-cluster",
			Generation: 1,
		},
	}

	if predicate.Update(event.UpdateEvent{ObjectOld: base, ObjectNew: base.DeepCopy()}) {
		t.Fatalf("expected same-generation update without refresh signal change to be ignored")
	}

	withSignal := base.DeepCopy()
	withSignal.Annotations = map[string]string{
		AnnotationRefreshClusterStats: string(ClusterStatsRefreshTypeAll),
	}
	if !predicate.Update(event.UpdateEvent{ObjectOld: base, ObjectNew: withSignal}) {
		t.Fatalf("expected refresh signal annotation-only update to trigger reconcile")
	}

	nextGeneration := base.DeepCopy()
	nextGeneration.Generation = 2
	if !predicate.Update(event.UpdateEvent{ObjectOld: base, ObjectNew: nextGeneration}) {
		t.Fatalf("expected generation change to trigger reconcile")
	}
}
