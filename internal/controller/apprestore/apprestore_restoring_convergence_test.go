package apprestore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	controller "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newRestoringConvergenceReconciler(t *testing.T) (*AppRestoreReconciler, *controller.MockClient, ctrlclient.Client) {
	t.Helper()

	scheme := newPVRTestScheme(t)
	mgmtClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	mockTargetClient := &controller.MockClient{
		Client: mgmtClient,
	}
	mockFactory := &controller.MockClientFactory{
		MockClient: mockTargetClient,
	}
	reconciler := &AppRestoreReconciler{
		Client:        mgmtClient,
		Scheme:        scheme,
		Recorder:      record.NewFakeRecorder(50),
		ClientFactory: mockFactory,
	}
	return reconciler, mockTargetClient, mgmtClient
}

func newRestoringTestAppRestore(name string) *disasterv1.AppRestore {
	return &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "disaster-system",
			UID:       types.UID("uid-" + name),
		},
		Spec: disasterv1.AppRestoreSpec{
			Cluster: "cluster-a",
		},
		Status: disasterv1.AppRestoreStatus{
			Status: disasterv1.PhaseRestoring,
		},
	}
}

func TestRestoringHandler_ProgressCompletedStallTriggersAutoRetry(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-progress-stall")
	appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseInProgress}
	restoreName := reconciler.GenRestoreName(appRestore)
	now := time.Now()

	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              restoreName,
			Namespace:         controller.VeleroNamespace,
			CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)),
		},
		Status: velerov1.RestoreStatus{
			Phase:          velerov1.RestorePhaseInProgress,
			StartTimestamp: &metav1.Time{Time: now.Add(-9 * time.Minute)},
			Progress: &velerov1.RestoreProgress{
				TotalItems:    14,
				ItemsRestored: 14,
			},
		},
	}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if target, ok := obj.(*velerov1.Restore); ok {
			restore.DeepCopyInto(target)
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}
	mockTargetClient.MockList = func(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
		if pvrList, ok := list.(*velerov1.PodVolumeRestoreList); ok {
			pvrList.Items = nil
			return nil
		}
		return mgmtClient.List(ctx, list, opts...)
	}

	deleteCalled := 0
	mockTargetClient.MockDelete = func(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.DeleteOption) error {
		if _, ok := obj.(*velerov1.Restore); ok {
			deleteCalled++
			return nil
		}
		return mgmtClient.Delete(ctx, obj, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, res, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseRestoring {
		t.Fatalf("expected next phase Restoring, got %q", nextPhase)
	}
	cfg := reconciler.restoreRuntimeConfig()
	if res.RequeueAfter != cfg.RetryBackoff {
		t.Fatalf("expected requeueAfter %s, got %s", cfg.RetryBackoff, res.RequeueAfter)
	}
	if deleteCalled != 1 {
		t.Fatalf("expected restore delete to be called once, got %d", deleteCalled)
	}
	if got := appRestore.Labels[labelAppRestoreRetryProgress]; got != "1" {
		t.Fatalf("expected progress retry count label to be 1, got %q", got)
	}
}

func TestRestoringHandler_GetRestoreTransientErrorKeepsRestoring(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-transient-get-error")
	appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseInProgress}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if _, ok := obj.(*velerov1.Restore); ok {
			return fmt.Errorf("the server was unable to return a response in the time allotted")
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, res, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseRestoring {
		t.Fatalf("expected next phase Restoring, got %q", nextPhase)
	}
	if res.RequeueAfter != reconciler.restoreRuntimeConfig().RetryBackoff {
		t.Fatalf("expected requeueAfter %s, got %s", reconciler.restoreRuntimeConfig().RetryBackoff, res.RequeueAfter)
	}
}

func TestIsTransientKubeAPIError_DetectsRestoreConnectionLoss(t *testing.T) {
	cases := []string{
		`Get "https://10.10.10.171:6443/apis/velero.io/v1/namespaces/velero/restores/res-a": net/http: TLS handshake timeout`,
		`Get "https://10.10.10.171:6443/apis/velero.io/v1/namespaces/velero/restores/res-a": http2: client connection lost`,
		`Get "https://10.10.10.171:6443/apis/velero.io/v1/namespaces/velero/restores/res-a": read tcp 10.0.0.1:12345->10.0.0.2:6443: connection reset by peer`,
		`Get "https://10.10.10.171:6443/apis/velero.io/v1/namespaces/velero/restores/res-a": unexpected EOF`,
	}
	for _, tc := range cases {
		if !isTransientKubeAPIError(errors.New(tc)) {
			t.Fatalf("expected transient error for %q", tc)
		}
	}
}

func TestRestoringHandler_GetRestoreNonTransientErrorFails(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-non-transient-get-error")
	appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseInProgress}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if _, ok := obj.(*velerov1.Restore); ok {
			return fmt.Errorf("permission denied")
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, _, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err == nil {
		t.Fatalf("expected non-transient Get Restore error")
	}
	if nextPhase != disasterv1.PhaseFailed {
		t.Fatalf("expected next phase Failed, got %q", nextPhase)
	}
}

func TestRestoringHandler_ProgressCompletedWithActivePVRDoesNotAutoRetry(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-progress-active-pvr")
	appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseInProgress}
	restoreName := reconciler.GenRestoreName(appRestore)
	now := time.Now()

	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              restoreName,
			Namespace:         controller.VeleroNamespace,
			CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)),
		},
		Status: velerov1.RestoreStatus{
			Phase:          velerov1.RestorePhaseInProgress,
			StartTimestamp: &metav1.Time{Time: now.Add(-9 * time.Minute)},
			Progress: &velerov1.RestoreProgress{
				TotalItems:    14,
				ItemsRestored: 14,
			},
		},
	}
	pvr := &velerov1.PodVolumeRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active-pvr",
			Namespace: controller.VeleroNamespace,
			Labels: map[string]string{
				velerov1.RestoreNameLabel: restoreName,
			},
		},
		Status: velerov1.PodVolumeRestoreStatus{
			Phase: velerov1.PodVolumeRestorePhaseInProgress,
		},
	}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if target, ok := obj.(*velerov1.Restore); ok {
			restore.DeepCopyInto(target)
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}
	mockTargetClient.MockList = func(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
		if pvrList, ok := list.(*velerov1.PodVolumeRestoreList); ok {
			pvrList.Items = []velerov1.PodVolumeRestore{*pvr}
			return nil
		}
		return mgmtClient.List(ctx, list, opts...)
	}

	deleteCalled := 0
	mockTargetClient.MockDelete = func(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.DeleteOption) error {
		if _, ok := obj.(*velerov1.Restore); ok {
			deleteCalled++
			return nil
		}
		return mgmtClient.Delete(ctx, obj, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, res, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseRestoring {
		t.Fatalf("expected next phase Restoring, got %q", nextPhase)
	}
	if res.RequeueAfter != controller.RestorePhaseInProgressWaitSeconds {
		t.Fatalf("expected regular in-progress requeue, got %s", res.RequeueAfter)
	}
	if deleteCalled != 0 {
		t.Fatalf("expected restore not to be deleted while PVR is active, got %d", deleteCalled)
	}
	if got := appRestore.Labels[labelAppRestoreRetryProgress]; got != "" {
		t.Fatalf("expected no progress retry count label, got %q", got)
	}
}

func TestRestoringHandler_StallAfterRetryFails(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-progress-stall-after-retry")
	appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseInProgress}
	appRestore.Labels = map[string]string{
		labelAppRestoreAutoRetryCount: "1",
	}
	restoreName := reconciler.GenRestoreName(appRestore)
	now := time.Now()

	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              restoreName,
			Namespace:         controller.VeleroNamespace,
			CreationTimestamp: metav1.NewTime(now.Add(-12 * time.Minute)),
		},
		Status: velerov1.RestoreStatus{
			Phase:          velerov1.RestorePhaseInProgress,
			StartTimestamp: &metav1.Time{Time: now.Add(-11 * time.Minute)},
			Progress: &velerov1.RestoreProgress{
				TotalItems:    14,
				ItemsRestored: 14,
			},
		},
	}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if target, ok := obj.(*velerov1.Restore); ok {
			restore.DeepCopyInto(target)
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}
	mockTargetClient.MockList = func(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
		if pvrList, ok := list.(*velerov1.PodVolumeRestoreList); ok {
			pvrList.Items = nil
			return nil
		}
		return mgmtClient.List(ctx, list, opts...)
	}
	deleteCalled := 0
	mockTargetClient.MockDelete = func(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.DeleteOption) error {
		if _, ok := obj.(*velerov1.Restore); ok {
			deleteCalled++
			return nil
		}
		return mgmtClient.Delete(ctx, obj, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, _, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseFailed {
		t.Fatalf("expected next phase Failed, got %q", nextPhase)
	}
	if appRestore.Status.Reason != "RestoreStalledAfterRetry" {
		t.Fatalf("expected reason RestoreStalledAfterRetry, got %q", appRestore.Status.Reason)
	}
	if !strings.Contains(appRestore.Status.Message, "stallType=progress_completed") {
		t.Fatalf("expected failure message to contain stallType=progress_completed, got %q", appRestore.Status.Message)
	}
	if deleteCalled == 0 {
		t.Fatalf("expected cleanup to attempt terminating restore when retry is exhausted")
	}
}

func TestRestoringHandler_StartupStallTriggersAutoRetry(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-startup-stall")
	restoreName := reconciler.GenRestoreName(appRestore)
	now := time.Now()

	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              restoreName,
			Namespace:         controller.VeleroNamespace,
			CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Minute)),
		},
		Status: velerov1.RestoreStatus{
			Phase: "",
		},
	}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if target, ok := obj.(*velerov1.Restore); ok {
			restore.DeepCopyInto(target)
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}
	deleteCalled := 0
	mockTargetClient.MockDelete = func(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.DeleteOption) error {
		if _, ok := obj.(*velerov1.Restore); ok {
			deleteCalled++
			return nil
		}
		return mgmtClient.Delete(ctx, obj, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, res, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseRestoring {
		t.Fatalf("expected next phase Restoring, got %q", nextPhase)
	}
	cfg := reconciler.restoreRuntimeConfig()
	if res.RequeueAfter != cfg.RetryBackoff {
		t.Fatalf("expected requeueAfter %s, got %s", cfg.RetryBackoff, res.RequeueAfter)
	}
	if deleteCalled != 1 {
		t.Fatalf("expected restore delete to be called once, got %d", deleteCalled)
	}
	if got := appRestore.Labels[labelAppRestoreRetryStartup]; got != "1" {
		t.Fatalf("expected startup retry count label to be 1, got %q", got)
	}
}

func TestRestoringHandler_NotFoundWithinGraceKeepsRestoring(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-notfound-within-grace")
	appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseInProgress}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if _, ok := obj.(*velerov1.Restore); ok {
			return apierrors.NewNotFound(velerov1.Resource("restore"), key.Name)
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}
	createRestoreCalled := 0
	mockTargetClient.MockCreate = func(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.CreateOption) error {
		if _, ok := obj.(*velerov1.Restore); ok {
			createRestoreCalled++
			return nil
		}
		return mgmtClient.Create(ctx, obj, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, res, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseRestoring {
		t.Fatalf("expected next phase Restoring, got %q", nextPhase)
	}
	cfg := reconciler.restoreRuntimeConfig()
	if res.RequeueAfter != cfg.RetryBackoff {
		t.Fatalf("expected requeueAfter %s, got %s", cfg.RetryBackoff, res.RequeueAfter)
	}
	if createRestoreCalled != 0 {
		t.Fatalf("expected restore not to be recreated within grace, got %d", createRestoreCalled)
	}
	if _, ok := getRestoreMissingSince(appRestore); !ok {
		t.Fatalf("expected missing-since marker to be set")
	}
}

func TestRestoringHandler_NotFoundExceedGraceTriggersAutoRetry(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-notfound-exceed-grace")
	appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseInProgress}
	appRestore.Annotations = map[string]string{
		annotationAppRestoreMissingSince: time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339),
	}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if _, ok := obj.(*velerov1.Restore); ok {
			return apierrors.NewNotFound(velerov1.Resource("restore"), key.Name)
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, res, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseRestoring {
		t.Fatalf("expected next phase Restoring, got %q", nextPhase)
	}
	cfg := reconciler.restoreRuntimeConfig()
	if res.RequeueAfter != cfg.RetryBackoff {
		t.Fatalf("expected requeueAfter %s, got %s", cfg.RetryBackoff, res.RequeueAfter)
	}
	if got := appRestore.Labels[labelAppRestoreRetryMissing]; got != "1" {
		t.Fatalf("expected missing retry count label to be 1, got %q", got)
	}
	if appRestore.Status.RestoreStatus.Phase != "" {
		t.Fatalf("expected restore status to be cleared after auto retry, got phase=%q", appRestore.Status.RestoreStatus.Phase)
	}
}

func TestRestoringHandler_EmptyStatusExceedGraceTriggersAutoRetry(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-empty-status-retry")
	restoreName := reconciler.GenRestoreName(appRestore)
	now := time.Now()

	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              restoreName,
			Namespace:         controller.VeleroNamespace,
			CreationTimestamp: metav1.NewTime(now.Add(-1 * time.Minute)),
			ResourceVersion:   "10",
		},
		Status: velerov1.RestoreStatus{
			Phase: "",
		},
	}
	appRestore.Annotations = map[string]string{
		annotationAppRestoreLastObservedSig: buildRestoreObservationSignature(restore),
		annotationAppRestoreLastProgressAt:  now.Add(-10 * time.Minute).UTC().Format(time.RFC3339),
	}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if target, ok := obj.(*velerov1.Restore); ok {
			restore.DeepCopyInto(target)
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}
	mockTargetClient.MockList = func(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
		if pvrList, ok := list.(*velerov1.PodVolumeRestoreList); ok {
			pvrList.Items = nil
			return nil
		}
		return mgmtClient.List(ctx, list, opts...)
	}

	deleteCalled := 0
	mockTargetClient.MockDelete = func(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.DeleteOption) error {
		if _, ok := obj.(*velerov1.Restore); ok {
			deleteCalled++
			return nil
		}
		return mgmtClient.Delete(ctx, obj, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, res, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseRestoring {
		t.Fatalf("expected next phase Restoring, got %q", nextPhase)
	}
	cfg := reconciler.restoreRuntimeConfig()
	if res.RequeueAfter != cfg.RetryBackoff {
		t.Fatalf("expected requeueAfter %s, got %s", cfg.RetryBackoff, res.RequeueAfter)
	}
	if deleteCalled != 1 {
		t.Fatalf("expected restore delete once, got %d", deleteCalled)
	}
	if got := appRestore.Labels[labelAppRestoreRetryEmpty]; got != "1" {
		t.Fatalf("expected empty-status retry count label to be 1, got %q", got)
	}
}

func TestRestoringHandler_EmptyStatusTrackingUsesAnnotations(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-empty-status-tracking")
	restoreName := reconciler.GenRestoreName(appRestore)
	now := time.Now()

	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              restoreName,
			Namespace:         controller.VeleroNamespace,
			CreationTimestamp: metav1.NewTime(now.Add(-1 * time.Minute)),
			ResourceVersion:   "12",
		},
		Status: velerov1.RestoreStatus{
			Phase: "",
		},
	}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if target, ok := obj.(*velerov1.Restore); ok {
			restore.DeepCopyInto(target)
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}
	mockTargetClient.MockList = func(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
		if pvrList, ok := list.(*velerov1.PodVolumeRestoreList); ok {
			pvrList.Items = nil
			return nil
		}
		return mgmtClient.List(ctx, list, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, _, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseRestoring {
		t.Fatalf("expected next phase Restoring, got %q", nextPhase)
	}
	if appRestore.Annotations == nil {
		t.Fatalf("expected annotations to be initialized")
	}
	if appRestore.Annotations[annotationAppRestoreLastObservedSig] == "" {
		t.Fatalf("expected last observed signature in annotations")
	}
	if appRestore.Annotations[annotationAppRestoreLastProgressAt] == "" {
		t.Fatalf("expected last progress timestamp in annotations")
	}
	if appRestore.Labels != nil {
		if _, exists := appRestore.Labels[annotationAppRestoreLastObservedSig]; exists {
			t.Fatalf("unexpected tracking signature key in labels")
		}
		if _, exists := appRestore.Labels[annotationAppRestoreLastProgressAt]; exists {
			t.Fatalf("unexpected tracking timestamp key in labels")
		}
	}
}

func TestRestoringHandler_EmptyStatusRetryExhaustedFails(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-empty-status-fail")
	restoreName := reconciler.GenRestoreName(appRestore)
	now := time.Now()

	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              restoreName,
			Namespace:         controller.VeleroNamespace,
			CreationTimestamp: metav1.NewTime(now.Add(-1 * time.Minute)),
			ResourceVersion:   "11",
		},
		Status: velerov1.RestoreStatus{
			Phase: "",
		},
	}
	appRestore.Annotations = map[string]string{
		annotationAppRestoreLastObservedSig: buildRestoreObservationSignature(restore),
		annotationAppRestoreLastProgressAt:  now.Add(-10 * time.Minute).UTC().Format(time.RFC3339),
	}
	appRestore.Labels = map[string]string{
		labelAppRestoreRetryEmpty: "2",
	}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if target, ok := obj.(*velerov1.Restore); ok {
			restore.DeepCopyInto(target)
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}
	mockTargetClient.MockList = func(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
		if pvrList, ok := list.(*velerov1.PodVolumeRestoreList); ok {
			pvrList.Items = nil
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
	if appRestore.Status.Reason != "RestoreStalledAfterRetry" {
		t.Fatalf("expected reason RestoreStalledAfterRetry, got %q", appRestore.Status.Reason)
	}
	if !strings.Contains(appRestore.Status.Message, "stallType=empty_status") {
		t.Fatalf("expected message contain stallType=empty_status, got %q", appRestore.Status.Message)
	}
}

func TestRestoringHandler_PerTypeRetryIndependent(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	appRestore := newRestoringTestAppRestore("apprestore-retry-independent")
	appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseInProgress}
	appRestore.Labels = map[string]string{
		labelAppRestoreRetryStartup: "1",
	}
	appRestore.Annotations = map[string]string{
		annotationAppRestoreMissingSince: time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339),
	}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if _, ok := obj.(*velerov1.Restore); ok {
			return apierrors.NewNotFound(velerov1.Resource("restore"), key.Name)
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}

	handler := &RestoringHandler{}
	nextPhase, _, err := handler.Handle(context.Background(), reconciler, appRestore)
	if err != nil {
		t.Fatalf("RestoringHandler.Handle returned error: %v", err)
	}
	if nextPhase != disasterv1.PhaseRestoring {
		t.Fatalf("expected next phase Restoring, got %q", nextPhase)
	}
	if got := appRestore.Labels[labelAppRestoreRetryMissing]; got != "1" {
		t.Fatalf("expected missing retry count label to be 1, got %q", got)
	}
	if got := appRestore.Labels[labelAppRestoreRetryStartup]; got != "1" {
		t.Fatalf("expected startup retry count to remain 1, got %q", got)
	}
}

func TestRestoringHandler_RestorePVsFalseStillDetectsPVRFailure(t *testing.T) {
	reconciler, mockTargetClient, mgmtClient := newRestoringConvergenceReconciler(t)
	restorePVsFalse := false
	appRestore := newRestoringTestAppRestore("apprestore-pvr-false")
	appRestore.Spec.Template.RestorePVs = &restorePVsFalse
	restoreName := reconciler.GenRestoreName(appRestore)
	now := time.Now()

	restore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              restoreName,
			Namespace:         controller.VeleroNamespace,
			CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Minute)),
		},
		Status: velerov1.RestoreStatus{
			Phase: velerov1.RestorePhaseInProgress,
		},
	}

	mockTargetClient.MockGet = func(ctx context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object, opts ...ctrlclient.GetOption) error {
		if target, ok := obj.(*velerov1.Restore); ok {
			restore.DeepCopyInto(target)
			return nil
		}
		return mgmtClient.Get(ctx, key, obj, opts...)
	}
	mockTargetClient.MockList = func(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
		if pvrList, ok := list.(*velerov1.PodVolumeRestoreList); ok {
			pvrList.Items = []velerov1.PodVolumeRestore{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pvr-failed-when-restorePVs-false",
						Namespace: controller.VeleroNamespace,
						Labels: map[string]string{
							"velero.io/restore-name": restoreName,
						},
						CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Minute)),
					},
					Status: velerov1.PodVolumeRestoreStatus{
						Phase:   velerov1.PodVolumeRestorePhaseFailed,
						Message: "simulated pvr failure",
					},
				},
			}
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
	if appRestore.Status.Reason != "PodVolumeRestoreFailed" {
		t.Fatalf("expected reason PodVolumeRestoreFailed, got %q", appRestore.Status.Reason)
	}
	if !strings.Contains(appRestore.Status.Message, "pvr-failed-when-restorePVs-false") {
		t.Fatalf("expected message to include failed pvr name, got %q", appRestore.Status.Message)
	}
}
