package apprestore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	controller "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRestoringHandler_FailsWhenStorageClassPatchNoEffect(t *testing.T) {
	scheme := newPVRTestScheme(t)
	mgmtClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockTargetClient := &controller.MockClient{Client: mgmtClient}
	mockFactory := &controller.MockClientFactory{MockClient: mockTargetClient}

	reconciler := &AppRestoreReconciler{
		Client:        mgmtClient,
		Scheme:        scheme,
		Recorder:      record.NewFakeRecorder(20),
		ClientFactory: mockFactory,
	}

	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "apprestore-noeffect",
			Namespace: "disaster-system",
			Annotations: map[string]string{
				"testudo.softcdata.com/modifier-summary": `{"flow":"forward"}`,
			},
		},
		Spec: disasterv1.AppRestoreSpec{
			Cluster: "cluster-a",
			ResourceModifierRules: []disasterv1.ResourceModifierRule{{
				Conditions: disasterv1.Conditions{
					GroupResource: "persistentvolumeclaims",
					Namespaces:    []string{"prod"},
				},
				Patches: []disasterv1.JSONPatch{{
					Operation: "replace",
					Path:      "/spec/storageClassName",
					Value:     "sc-target",
				}},
			}},
		},
		Status: disasterv1.AppRestoreStatus{Status: disasterv1.PhaseRestoring},
	}

	restoreName := reconciler.GenRestoreName(appRestore)
	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if restore, ok := obj.(*velerov1.Restore); ok {
			now := metav1.NewTime(time.Now())
			restore.Name = restoreName
			restore.Namespace = controller.VeleroNamespace
			restore.Status.Phase = velerov1.RestorePhaseCompleted
			restore.Status.StartTimestamp = &now
			restore.Status.CompletionTimestamp = &now
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}
	mockTargetClient.MockList = func(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
		if pvcList, ok := list.(*corev1.PersistentVolumeClaimList); ok {
			cls := "local-path"
			pvcList.Items = []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "prod"},
				Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: &cls},
			}}
			return nil
		}
		return mgmtClient.List(ctx, list, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, _, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseFailed {
		t.Fatalf("expected next phase Failed, got %q", nextPhase)
	}
	if appRestore.Status.Reason != modifierEffectReasonNoEffect {
		t.Fatalf("expected reason %s, got %q", modifierEffectReasonNoEffect, appRestore.Status.Reason)
	}
	if !strings.Contains(appRestore.Status.Message, "resource=prod/data") {
		t.Fatalf("expected message to include pvc diff, got %q", appRestore.Status.Message)
	}

	summaryRaw := appRestore.Annotations["testudo.softcdata.com/modifier-summary"]
	summary := map[string]any{}
	if err := json.Unmarshal([]byte(summaryRaw), &summary); err != nil {
		t.Fatalf("unmarshal modifier summary: %v", err)
	}
	if got := int(summary["effectiveRuleCount"].(float64)); got != 0 {
		t.Fatalf("expected effectiveRuleCount=0, got %d", got)
	}
	if got := int(summary["noEffectRuleCount"].(float64)); got != 1 {
		t.Fatalf("expected noEffectRuleCount=1, got %d", got)
	}
}

func TestRestoringHandler_SucceedsWhenStorageClassPatchEffective(t *testing.T) {
	scheme := newPVRTestScheme(t)
	mgmtClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockTargetClient := &controller.MockClient{Client: mgmtClient}
	mockFactory := &controller.MockClientFactory{MockClient: mockTargetClient}

	reconciler := &AppRestoreReconciler{
		Client:        mgmtClient,
		Scheme:        scheme,
		Recorder:      record.NewFakeRecorder(20),
		ClientFactory: mockFactory,
	}

	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "apprestore-effective",
			Namespace: "disaster-system",
		},
		Spec: disasterv1.AppRestoreSpec{
			Cluster: "cluster-a",
			ResourceModifierRules: []disasterv1.ResourceModifierRule{{
				Conditions: disasterv1.Conditions{
					GroupResource: "persistentvolumeclaims",
					Namespaces:    []string{"prod"},
				},
				Patches: []disasterv1.JSONPatch{{
					Operation: "replace",
					Path:      "/spec/storageClassName",
					Value:     "sc-target",
				}},
			}},
		},
		Status: disasterv1.AppRestoreStatus{Status: disasterv1.PhaseRestoring},
	}

	restoreName := reconciler.GenRestoreName(appRestore)
	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if restore, ok := obj.(*velerov1.Restore); ok {
			now := metav1.NewTime(time.Now())
			restore.Name = restoreName
			restore.Namespace = controller.VeleroNamespace
			restore.Status.Phase = velerov1.RestorePhaseCompleted
			restore.Status.StartTimestamp = &now
			restore.Status.CompletionTimestamp = &now
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}
	mockTargetClient.MockList = func(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
		if pvcList, ok := list.(*corev1.PersistentVolumeClaimList); ok {
			cls := "sc-target"
			pvcList.Items = []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "prod"},
				Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: &cls},
			}}
			return nil
		}
		return mgmtClient.List(ctx, list, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, _, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseSucceeded {
		t.Fatalf("expected next phase Succeeded, got %q", nextPhase)
	}

	summaryRaw := appRestore.Annotations["testudo.softcdata.com/modifier-summary"]
	summary := map[string]any{}
	if err := json.Unmarshal([]byte(summaryRaw), &summary); err != nil {
		t.Fatalf("unmarshal modifier summary: %v", err)
	}
	if got := int(summary["effectiveRuleCount"].(float64)); got != 1 {
		t.Fatalf("expected effectiveRuleCount=1, got %d", got)
	}
	if got := int(summary["noEffectRuleCount"].(float64)); got != 0 {
		t.Fatalf("expected noEffectRuleCount=0, got %d", got)
	}
}

func TestRestoringHandler_FailsWhenStorageClassPatchNoEffect_Reverse(t *testing.T) {
	scheme := newPVRTestScheme(t)
	mgmtClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockTargetClient := &controller.MockClient{Client: mgmtClient}
	mockFactory := &controller.MockClientFactory{MockClient: mockTargetClient}

	reconciler := &AppRestoreReconciler{
		Client:        mgmtClient,
		Scheme:        scheme,
		Recorder:      record.NewFakeRecorder(20),
		ClientFactory: mockFactory,
	}

	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "apprestore-noeffect-reverse",
			Namespace: "disaster-system",
			Annotations: map[string]string{
				"testudo.softcdata.com/modifier-summary": `{"flow":"reverse"}`,
			},
		},
		Spec: disasterv1.AppRestoreSpec{
			Cluster: "cluster-a",
			ResourceModifierRules: []disasterv1.ResourceModifierRule{{
				Conditions: disasterv1.Conditions{
					GroupResource: "persistentvolumeclaims",
					Namespaces:    []string{"prod"},
				},
				Patches: []disasterv1.JSONPatch{{
					Operation: "replace",
					Path:      "/spec/storageClassName",
					Value:     "sc-source",
				}},
			}},
		},
		Status: disasterv1.AppRestoreStatus{Status: disasterv1.PhaseRestoring},
	}

	restoreName := reconciler.GenRestoreName(appRestore)
	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if restore, ok := obj.(*velerov1.Restore); ok {
			now := metav1.NewTime(time.Now())
			restore.Name = restoreName
			restore.Namespace = controller.VeleroNamespace
			restore.Status.Phase = velerov1.RestorePhaseCompleted
			restore.Status.StartTimestamp = &now
			restore.Status.CompletionTimestamp = &now
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}
	mockTargetClient.MockList = func(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
		if pvcList, ok := list.(*corev1.PersistentVolumeClaimList); ok {
			cls := "local-path"
			pvcList.Items = []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "prod"},
				Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: &cls},
			}}
			return nil
		}
		return mgmtClient.List(ctx, list, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, _, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseFailed {
		t.Fatalf("expected next phase Failed, got %q", nextPhase)
	}
	if appRestore.Status.Reason != modifierEffectReasonNoEffect {
		t.Fatalf("expected reason %s, got %q", modifierEffectReasonNoEffect, appRestore.Status.Reason)
	}

	summaryRaw := appRestore.Annotations["testudo.softcdata.com/modifier-summary"]
	summary := map[string]any{}
	if err := json.Unmarshal([]byte(summaryRaw), &summary); err != nil {
		t.Fatalf("unmarshal modifier summary: %v", err)
	}
	if got := int(summary["effectiveRuleCount"].(float64)); got != 0 {
		t.Fatalf("expected effectiveRuleCount=0, got %d", got)
	}
	if got := int(summary["noEffectRuleCount"].(float64)); got != 1 {
		t.Fatalf("expected noEffectRuleCount=1, got %d", got)
	}
}

func TestRestoringHandler_SucceedsWhenStorageClassPatchEffective_Reverse(t *testing.T) {
	scheme := newPVRTestScheme(t)
	mgmtClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mockTargetClient := &controller.MockClient{Client: mgmtClient}
	mockFactory := &controller.MockClientFactory{MockClient: mockTargetClient}

	reconciler := &AppRestoreReconciler{
		Client:        mgmtClient,
		Scheme:        scheme,
		Recorder:      record.NewFakeRecorder(20),
		ClientFactory: mockFactory,
	}

	appRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "apprestore-effective-reverse",
			Namespace: "disaster-system",
		},
		Spec: disasterv1.AppRestoreSpec{
			Cluster: "cluster-a",
			ResourceModifierRules: []disasterv1.ResourceModifierRule{{
				Conditions: disasterv1.Conditions{
					GroupResource: "persistentvolumeclaims",
					Namespaces:    []string{"prod"},
				},
				Patches: []disasterv1.JSONPatch{{
					Operation: "replace",
					Path:      "/spec/storageClassName",
					Value:     "sc-source",
				}},
			}},
		},
		Status: disasterv1.AppRestoreStatus{Status: disasterv1.PhaseRestoring},
	}

	restoreName := reconciler.GenRestoreName(appRestore)
	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if restore, ok := obj.(*velerov1.Restore); ok {
			now := metav1.NewTime(time.Now())
			restore.Name = restoreName
			restore.Namespace = controller.VeleroNamespace
			restore.Status.Phase = velerov1.RestorePhaseCompleted
			restore.Status.StartTimestamp = &now
			restore.Status.CompletionTimestamp = &now
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}
	mockTargetClient.MockList = func(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
		if pvcList, ok := list.(*corev1.PersistentVolumeClaimList); ok {
			cls := "sc-source"
			pvcList.Items = []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "prod"},
				Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: &cls},
			}}
			return nil
		}
		return mgmtClient.List(ctx, list, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, _, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseSucceeded {
		t.Fatalf("expected next phase Succeeded, got %q", nextPhase)
	}

	summaryRaw := appRestore.Annotations["testudo.softcdata.com/modifier-summary"]
	summary := map[string]any{}
	if err := json.Unmarshal([]byte(summaryRaw), &summary); err != nil {
		t.Fatalf("unmarshal modifier summary: %v", err)
	}
	if got := int(summary["effectiveRuleCount"].(float64)); got != 1 {
		t.Fatalf("expected effectiveRuleCount=1, got %d", got)
	}
	if got := int(summary["noEffectRuleCount"].(float64)); got != 0 {
		t.Fatalf("expected noEffectRuleCount=0, got %d", got)
	}
}
