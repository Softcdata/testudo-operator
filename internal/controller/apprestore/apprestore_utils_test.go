package apprestore

import (
	"context"
	"testing"
	"time"

	controller "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetVeleroRestore_FallbackByUIDSelectsLatest(t *testing.T) {
	scheme := newPVRTestScheme(t)
	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "apprestore-uid-fallback",
			Namespace: "disaster-system",
			UID:       types.UID("uid-apprestore-uid-fallback"),
		},
	}
	older := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "restore-older",
			Namespace:         controller.VeleroNamespace,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
			Labels: map[string]string{
				LabelAppRestoreUID: string(appRestore.UID),
			},
		},
	}
	newer := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "restore-newer",
			Namespace:         controller.VeleroNamespace,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
			Labels: map[string]string{
				LabelAppRestoreUID: string(appRestore.UID),
			},
		},
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(older, newer).Build()
	reconciler := &AppRestoreReconciler{}

	got, err := reconciler.getVeleroRestore(context.Background(), cli, appRestore)
	if err != nil {
		t.Fatalf("getVeleroRestore returned error: %v", err)
	}
	if got == nil {
		t.Fatalf("expected restore, got nil")
	}
	if got.Name != newer.Name {
		t.Fatalf("expected latest restore %q, got %q", newer.Name, got.Name)
	}
}

func TestGetVeleroRestore_FallbackLegacyName(t *testing.T) {
	scheme := newPVRTestScheme(t)
	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "apprestore-legacy-fallback",
			Namespace: "disaster-system",
			UID:       types.UID("uid-apprestore-legacy-fallback"),
		},
	}
	legacy := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-restore-" + appRestore.Name,
			Namespace: controller.VeleroNamespace,
		},
	}

	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(legacy).Build()
	reconciler := &AppRestoreReconciler{}

	got, err := reconciler.getVeleroRestore(context.Background(), cli, appRestore)
	if err != nil {
		t.Fatalf("getVeleroRestore returned error: %v", err)
	}
	if got == nil {
		t.Fatalf("expected restore, got nil")
	}
	if got.Name != legacy.Name {
		t.Fatalf("expected legacy restore %q, got %q", legacy.Name, got.Name)
	}
}
