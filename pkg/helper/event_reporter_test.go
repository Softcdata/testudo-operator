package helper

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/metadata"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

func TestCalculateDuration(t *testing.T) {
	now := time.Now()
	startTime := metav1.NewTime(now.Add(-10 * time.Minute))
	endTime := metav1.NewTime(now)

	tests := []struct {
		name      string
		startTime *metav1.Time
		endTime   *metav1.Time
		want      string
	}{
		{
			name:      "10 minutes difference",
			startTime: &startTime,
			endTime:   &endTime,
			want:      "10m0s",
		},
		{
			name:      "Negative difference",
			startTime: &endTime,
			endTime:   &startTime,
			want:      "0s",
		},
		{
			name:      "Nil startTime",
			startTime: nil,
			endTime:   &endTime,
			want:      "-",
		},
		{
			name:      "Nil endTime (use now)",
			startTime: &startTime,
			endTime:   nil,
			want:      "10m0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "Nil endTime (use now)" {
				got := CalculateDuration(tt.startTime, tt.endTime)
				if got == "" || got == "-" {
					t.Errorf("CalculateDuration() = %v, want non-empty duration", got)
				}
				return
			}
			if got := CalculateDuration(tt.startTime, tt.endTime); got != tt.want {
				t.Errorf("CalculateDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsTerminalPhase(t *testing.T) {
	tests := []struct {
		phase string
		want  bool
	}{
		{"Completed", true},
		{"Failed", true},
		{"PartiallyFailed", true},
		{"Canceled", true},
		{"Succeeded", true},
		{"InProgress", false},
		{"New", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			if got := IsTerminalPhase(tt.phase); got != tt.want {
				t.Errorf("IsTerminalPhase(%v) = %v, want %v", tt.phase, got, tt.want)
			}
		})
	}
}

func TestReportTask(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	obj := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod"}}

	t.Run("ReportTaskStarted with cluster", func(t *testing.T) {
		ReportTaskStarted(recorder, obj, "TestTask", "cluster-1", "user-1", "trace-1", "start msg")
		select {
		case ev := <-recorder.Events:
			if !strings.Contains(ev, "[Cluster: cluster-1]") {
				t.Errorf("Expected cluster in event, got: %s", ev)
			}
			if !strings.Contains(ev, "[User: user-1]") {
				t.Errorf("Expected user in event, got: %s", ev)
			}
			if !strings.Contains(ev, "[TraceID: trace-1]") {
				t.Errorf("Expected TraceID in event, got: %s", ev)
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for event")
		}
	})

	t.Run("ReportTaskFinished with cluster", func(t *testing.T) {
		now := metav1.Now()
		ReportTaskFinished(recorder, obj, "TestTask", "cluster-2", TaskStatusSuccess, &now, &now, "user-2", "trace-2", "finish msg")
		select {
		case ev := <-recorder.Events:
			if !strings.Contains(ev, "[Cluster: cluster-2]") {
				t.Errorf("Expected cluster in event, got: %s", ev)
			}
			if !strings.Contains(ev, "[Status: Success]") {
				t.Errorf("Expected status Success in event, got: %s", ev)
			}
			if !strings.Contains(ev, "[TraceID: trace-2]") {
				t.Errorf("Expected TraceID in event, got: %s", ev)
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for event")
		}
	})
}

func TestBuildTaskOriginLabels(t *testing.T) {
	controllerTrue := true

	dsBackup := &disasterv1.AppBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ds-backup",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "DataSync",
					Name:       "ds-1",
					Controller: &controllerTrue,
				},
			},
		},
	}

	labels := buildTaskOriginLabels(dsBackup)
	if labels[metadata.LabelTaskOrigin] != metadata.AppResourceOriginDisasterInstance {
		t.Fatalf("task-origin = %s, want %s", labels[metadata.LabelTaskOrigin], metadata.AppResourceOriginDisasterInstance)
	}
	if labels[metadata.LabelTaskOriginKind] != metadata.AppResourceOwnerKindDataSync {
		t.Fatalf("task-origin-kind = %s, want %s", labels[metadata.LabelTaskOriginKind], metadata.AppResourceOwnerKindDataSync)
	}

	userRestore := &disasterv1.AppRestore{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-restore"},
	}

	labels = buildTaskOriginLabels(userRestore)
	if labels[metadata.LabelTaskOrigin] != metadata.AppResourceOriginUser {
		t.Fatalf("task-origin = %s, want %s", labels[metadata.LabelTaskOrigin], metadata.AppResourceOriginUser)
	}
	if labels[metadata.LabelTaskOriginKind] != metadata.AppResourceOwnerKindUser {
		t.Fatalf("task-origin-kind = %s, want %s", labels[metadata.LabelTaskOriginKind], metadata.AppResourceOwnerKindUser)
	}
}

func TestShouldEmitTaskEvent_ProgressDedupe(t *testing.T) {
	recentTaskEvents = sync.Map{}
	ref := &corev1.ObjectReference{
		Namespace: "disaster-system",
		Name:      "op-1",
		Kind:      "DisasterOperation",
		UID:       "uid-1",
	}
	eventType := corev1.EventTypeNormal
	reason := EventReasonExecutionProgress
	message := `{"task":"执行容灾实例操作","status":"InProgress","message":"等待同步完成"}`

	if !shouldEmitTaskEvent(ref, eventType, reason, message) {
		t.Fatalf("first progress event should be emitted")
	}
	if shouldEmitTaskEvent(ref, eventType, reason, message) {
		t.Fatalf("duplicate progress event inside dedupe window should be suppressed")
	}

	key := fmt.Sprintf("%s/%s|%s|%s|%s|%s|%s", ref.Namespace, ref.Name, ref.Kind, ref.UID, eventType, reason, message)
	recentTaskEvents.Store(key, time.Now().Add(-31*time.Second))
	if !shouldEmitTaskEvent(ref, eventType, reason, message) {
		t.Fatalf("progress event should be emitted again after dedupe window")
	}
}

func TestShouldEmitTaskEvent_DifferentMessageAlwaysPasses(t *testing.T) {
	recentTaskEvents = sync.Map{}
	ref := &corev1.ObjectReference{
		Namespace: "disaster-system",
		Name:      "op-2",
		Kind:      "DisasterOperation",
		UID:       "uid-2",
	}
	eventType := corev1.EventTypeNormal
	reason := EventReasonExecutionProgress

	if !shouldEmitTaskEvent(ref, eventType, reason, `{"message":"A"}`) {
		t.Fatalf("first progress event should be emitted")
	}
	if !shouldEmitTaskEvent(ref, eventType, reason, `{"message":"B"}`) {
		t.Fatalf("different message should not be suppressed")
	}
}

func TestShouldEmitTaskEvent_UIDMissingSkipsDedupe(t *testing.T) {
	recentTaskEvents = sync.Map{}
	ref := &corev1.ObjectReference{
		Namespace: "disaster-system",
		Name:      "op-no-uid",
		Kind:      "DisasterOperation",
		UID:       "",
	}
	eventType := corev1.EventTypeNormal
	reason := EventReasonExecutionStarted
	message := `{"task":"执行容灾实例操作","status":"InProgress","message":"开始执行"}`

	if !shouldEmitTaskEvent(ref, eventType, reason, message) {
		t.Fatalf("event with empty UID should be emitted")
	}
	if !shouldEmitTaskEvent(ref, eventType, reason, message) {
		t.Fatalf("event with empty UID should not be deduped")
	}
}

func TestShouldEmitDiagnosticEvent_WarningDedupeWindow(t *testing.T) {
	recentDiagnosticEvents = sync.Map{}
	obj := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "disaster-system",
			Name:      "pod-1",
			UID:       "uid-warning-1",
		},
	}
	eventType := corev1.EventTypeWarning
	reason := "SyncFailed"
	message := "sync failed"

	if !shouldEmitDiagnosticEvent(obj, eventType, reason, message) {
		t.Fatalf("first warning diagnostic event should be emitted")
	}
	if shouldEmitDiagnosticEvent(obj, eventType, reason, message) {
		t.Fatalf("duplicate warning diagnostic event inside dedupe window should be suppressed")
	}

	key, ok := buildDiagnosticEventKey(obj, eventType, reason, message)
	if !ok {
		t.Fatalf("failed to build diagnostic event key")
	}
	recentDiagnosticEvents.Store(key, time.Now().Add(-31*time.Second))
	if !shouldEmitDiagnosticEvent(obj, eventType, reason, message) {
		t.Fatalf("warning diagnostic event should be emitted again after dedupe window")
	}
}

func TestShouldEmitDiagnosticEvent_NormalDedupeWindow(t *testing.T) {
	recentDiagnosticEvents = sync.Map{}
	obj := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "disaster-system",
			Name:      "pod-2",
			UID:       "uid-normal-1",
		},
	}
	eventType := corev1.EventTypeNormal
	reason := "Progress"
	message := "waiting next check"

	if !shouldEmitDiagnosticEvent(obj, eventType, reason, message) {
		t.Fatalf("first normal diagnostic event should be emitted")
	}
	if shouldEmitDiagnosticEvent(obj, eventType, reason, message) {
		t.Fatalf("duplicate normal diagnostic event inside dedupe window should be suppressed")
	}

	key, ok := buildDiagnosticEventKey(obj, eventType, reason, message)
	if !ok {
		t.Fatalf("failed to build diagnostic event key")
	}
	recentDiagnosticEvents.Store(key, time.Now().Add(-11*time.Second))
	if !shouldEmitDiagnosticEvent(obj, eventType, reason, message) {
		t.Fatalf("normal diagnostic event should be emitted again after dedupe window")
	}
}
