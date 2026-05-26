package metadata

import "testing"

func TestEnsureCleanupLabels(t *testing.T) {
	labels, changed := EnsureCleanupLabels(nil, CleanupDescriptor{
		OwnerUID:     "owner-uid-1",
		RelationCode: "finalizer.veleroSchedule",
		Strategy:     CleanupStrategyDelete,
	})
	if !changed {
		t.Fatalf("expected labels to change")
	}
	if labels[LabelCleanupOwnerToken] == "" {
		t.Fatalf("expected cleanup owner token")
	}
	if labels[LabelCleanupRelation] != "finalizer.veleroSchedule" {
		t.Fatalf("unexpected cleanup relation: %s", labels[LabelCleanupRelation])
	}
	if labels[LabelCleanupStrategy] != CleanupStrategyDelete {
		t.Fatalf("unexpected cleanup strategy: %s", labels[LabelCleanupStrategy])
	}
	if labels[LabelCleanupManagedBy] != LabelCleanupManagedByValueOperator {
		t.Fatalf("unexpected cleanup managed-by: %s", labels[LabelCleanupManagedBy])
	}

	labels2, changed2 := EnsureCleanupLabels(labels, CleanupDescriptor{
		OwnerUID:     "owner-uid-1",
		RelationCode: "finalizer.veleroSchedule",
		Strategy:     CleanupStrategyDelete,
	})
	if changed2 {
		t.Fatalf("expected labels to stay unchanged")
	}
	if labels2[LabelCleanupOwnerToken] != labels[LabelCleanupOwnerToken] {
		t.Fatalf("expected cleanup owner token to be stable")
	}
}
