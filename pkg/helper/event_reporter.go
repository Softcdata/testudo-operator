package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/softcdata/testudo-operator/pkg/metadata"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/reference"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// EventReasonExecutionStarted 任务开始执行
	EventReasonExecutionStarted = "ExecutionStarted"
	// EventReasonExecutionFinished 任务执行结束
	EventReasonExecutionFinished = "ExecutionFinished"
	// EventReasonExecutionProgress 任务执行进度
	EventReasonExecutionProgress = "ExecutionProgress"

	// TaskStatusInProgress 任务进行中
	TaskStatusInProgress = "InProgress"
	// TaskStatusSuccess 任务成功
	TaskStatusSuccess = "Success"
	// TaskStatusFailed 任务失败
	TaskStatusFailed = "Failed"
	// TaskStatusCanceled 任务已取消
	TaskStatusCanceled = "Canceled"

	// LabelTaskEvent 标识是否为结构化任务事件
	LabelTaskEvent = "testudo.softcdata.com/task-event"

	// DefaultEventNamespace 默认事件命名空间 (用于非命名空间资源)
	DefaultEventNamespace = "disaster-system"
)

var recentTaskEvents sync.Map
var recentDiagnosticEvents sync.Map
var defaultEventNamespace = DefaultEventNamespace

func SetDefaultEventNamespace(namespace string) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = DefaultEventNamespace
	}
	defaultEventNamespace = namespace
}

func EffectiveDefaultEventNamespace() string {
	if namespace := strings.TrimSpace(defaultEventNamespace); namespace != "" {
		return namespace
	}
	return DefaultEventNamespace
}

func eventDedupeWindow(reason string) time.Duration {
	switch reason {
	case EventReasonExecutionProgress:
		// 进度类事件在等待循环中最容易刷屏，窗口设置更长。
		return 30 * time.Second
	case EventReasonExecutionStarted, EventReasonExecutionFinished:
		// 启停事件保留短窗口，用于吸收并发冲突导致的重复上报。
		return 5 * time.Second
	default:
		return 0
	}
}

func shouldEmitTaskEvent(ref *corev1.ObjectReference, eventType, reason, message string) bool {
	window := eventDedupeWindow(reason)
	if window <= 0 {
		return true
	}
	// UID 缺失时无法稳定标识对象实例，避免误抑制不同批次事件。
	if ref == nil || ref.UID == "" {
		return true
	}
	key := fmt.Sprintf("%s/%s|%s|%s|%s|%s|%s", ref.Namespace, ref.Name, ref.Kind, ref.UID, eventType, reason, message)
	now := time.Now()
	if last, ok := recentTaskEvents.Load(key); ok {
		if ts, ok := last.(time.Time); ok && now.Sub(ts) < window {
			return false
		}
	}
	recentTaskEvents.Store(key, now)
	return true
}

func diagnosticEventDedupeWindow(eventType string) time.Duration {
	if eventType == corev1.EventTypeWarning {
		return 30 * time.Second
	}
	return 10 * time.Second
}

func buildDiagnosticEventKey(object runtime.Object, eventType, reason, message string) (string, bool) {
	accessor, err := apimeta.Accessor(object)
	if err != nil {
		return "", false
	}
	kind := object.GetObjectKind().GroupVersionKind().Kind
	if kind == "" {
		kind = fmt.Sprintf("%T", object)
	}
	return fmt.Sprintf("%s/%s|%s|%s|%s|%s|%s", accessor.GetNamespace(), accessor.GetName(), kind, accessor.GetUID(), eventType, reason, message), true
}

func shouldEmitDiagnosticEvent(object runtime.Object, eventType, reason, message string) bool {
	window := diagnosticEventDedupeWindow(eventType)
	if window <= 0 {
		return true
	}
	key, ok := buildDiagnosticEventKey(object, eventType, reason, message)
	if !ok {
		return true
	}
	now := time.Now()
	if last, ok := recentDiagnosticEvents.Load(key); ok {
		if ts, ok := last.(time.Time); ok && now.Sub(ts) < window {
			return false
		}
	}
	recentDiagnosticEvents.Store(key, now)
	return true
}

// DisasterEventPayload 定义结构化事件消息的 JSON 载荷
type DisasterEventPayload struct {
	Task     string `json:"task"`
	Status   string `json:"status"` // "InProgress", "Success", "Failed"
	Cluster  string `json:"cluster,omitempty"`
	User     string `json:"user,omitempty"`
	TraceID  string `json:"traceId,omitempty"`
	Duration string `json:"duration,omitempty"` // 用于 Finished 事件
	Code     string `json:"code,omitempty"`     // 稳定语义节点编码
	// ErrorCode 机器可读错误码，仅失败结束事件填写
	ErrorCode string `json:"errorCode,omitempty"`
	Message   string `json:"message"` // 实际的人类可读消息
}

// CalculateDuration 计算两个时间点之间的时间差，并格式化为人类可读的字符串
func CalculateDuration(startTime, endTime *metav1.Time) string {
	if startTime == nil {
		return "-"
	}
	var end time.Time
	if endTime == nil {
		end = time.Now()
	} else {
		end = endTime.Time
	}

	duration := end.Sub(startTime.Time)
	if duration < 0 {
		return "0s"
	}
	return duration.Round(time.Second).String()
}

// ReportTaskStartedWithClient 使用 Client 直接创建带 Label 的 Event，以支持 Server 端高效查询
func ReportTaskStartedWithClient(ctx context.Context, c client.Client, scheme *runtime.Scheme, object runtime.Object, taskName string, cluster string, user string, traceID string, extraMsg string) {
	if user == "" {
		user = "system"
	}
	if cluster == "" {
		cluster = "-"
	}
	if traceID == "" {
		traceID = "-"
	}

	payload := DisasterEventPayload{
		Task:    taskName,
		Status:  TaskStatusInProgress,
		Cluster: cluster,
		User:    user,
		TraceID: traceID,
		Message: extraMsg,
	}
	jsonBytes, _ := json.Marshal(payload)
	msg := string(jsonBytes)

	emitEventWithLabel(ctx, c, scheme, object, corev1.EventTypeNormal, EventReasonExecutionStarted, msg)
}

// ReportTaskFinishedWithClient 发射任务结束事件（带 Label）
func ReportTaskFinishedWithClient(ctx context.Context, c client.Client, scheme *runtime.Scheme, object runtime.Object, taskName string, cluster string, status string, startTime, endTime *metav1.Time, user string, traceID string, extraMsg string, errorCode ...string) {
	if user == "" {
		user = "system"
	}
	if cluster == "" {
		cluster = "-"
	}
	if traceID == "" {
		traceID = "-"
	}
	duration := CalculateDuration(startTime, endTime)

	payload := DisasterEventPayload{
		Task:     taskName,
		Status:   status,
		Cluster:  cluster,
		User:     user,
		TraceID:  traceID,
		Duration: duration,
		Message:  extraMsg,
	}
	if len(errorCode) > 0 {
		payload.ErrorCode = errorCode[0]
	}
	jsonBytes, _ := json.Marshal(payload)
	msg := string(jsonBytes)

	eventType := corev1.EventTypeNormal
	if status == TaskStatusFailed {
		eventType = corev1.EventTypeWarning
	}

	emitEventWithLabel(ctx, c, scheme, object, eventType, EventReasonExecutionFinished, msg)
}

// ReportTaskProgressWithClient 发射任务进度事件（带 Label）
func ReportTaskProgressWithClient(ctx context.Context, c client.Client, scheme *runtime.Scheme, object runtime.Object, taskName string, cluster string, user string, traceID string, progressMsg string, code ...string) {
	if user == "" {
		user = "system"
	}
	if cluster == "" {
		cluster = "-"
	}
	if traceID == "" {
		traceID = "-"
	}

	payload := DisasterEventPayload{
		Task:    taskName,
		Status:  TaskStatusInProgress,
		Cluster: cluster,
		User:    user,
		TraceID: traceID,
		Message: progressMsg,
	}
	if len(code) > 0 {
		payload.Code = code[0]
	}
	jsonBytes, _ := json.Marshal(payload)
	msg := string(jsonBytes)

	emitEventWithLabel(ctx, c, scheme, object, corev1.EventTypeNormal, EventReasonExecutionProgress, msg)
}

func emitEventWithLabel(ctx context.Context, c client.Client, scheme *runtime.Scheme, object runtime.Object, eventType, reason, message string) {
	ref, err := reference.GetReference(scheme, object)
	if err != nil {
		fmt.Printf("Failed to get reference: %v\n", err)
		return
	}

	namespace := ref.Namespace
	if namespace == "" {
		namespace = EffectiveDefaultEventNamespace()
		// For cluster-scoped resources, K8s requires involvedObject.namespace to match event.namespace
		ref.Namespace = namespace
	}
	if !shouldEmitTaskEvent(ref, eventType, reason, message) {
		return
	}
	fmt.Printf("Creating event in namespace: %s for object: %s/%s\n", namespace, ref.Kind, ref.Name)

	labels := map[string]string{
		LabelTaskEvent: "true",
	}
	for key, value := range buildTaskOriginLabels(object) {
		labels[key] = value
	}

	t := metav1.Now()
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: reason + "-",
			Namespace:    namespace,
			Labels:       labels,
		},
		InvolvedObject: *ref,
		Reason:         reason,
		Message:        message,
		Type:           eventType,
		FirstTimestamp: t,
		LastTimestamp:  t,
		Count:          1,
		Source: corev1.EventSource{
			Component: "disaster-operator",
		},
	}

	err = c.Create(ctx, event)
	if err != nil {
		fmt.Printf("Failed to create event: %v\n", err)
	}
}

func buildTaskOriginLabels(object runtime.Object) map[string]string {
	origin := metadata.AppResourceOriginUser
	originKind := metadata.AppResourceOwnerKindUser

	accessor, err := apimeta.Accessor(object)
	if err == nil {
		origin, originKind, _ = metadata.ResolveAppResourceOriginByOwnerRefs(accessor.GetOwnerReferences())
	}

	return map[string]string{
		metadata.LabelTaskOrigin:     origin,
		metadata.LabelTaskOriginKind: originKind,
	}
}

// ReportDiagnosticEvent 发射诊断事件，并在短窗口内抑制相同对象+reason+message 的重复上报。
func ReportDiagnosticEvent(recorder record.EventRecorder, object runtime.Object, eventType, reason, message string) {
	if recorder == nil || object == nil {
		return
	}
	if !shouldEmitDiagnosticEvent(object, eventType, reason, message) {
		return
	}
	recorder.Event(object, eventType, reason, message)
}

// ReportDiagnosticEventf 是 ReportDiagnosticEvent 的格式化版本。
func ReportDiagnosticEventf(recorder record.EventRecorder, object runtime.Object, eventType, reason, format string, args ...interface{}) {
	ReportDiagnosticEvent(recorder, object, eventType, reason, fmt.Sprintf(format, args...))
}

// ReportTaskStarted 发射任务启动事件 (旧版本，不支持 Label)
func ReportTaskStarted(recorder record.EventRecorder, object runtime.Object, taskName string, cluster string, user string, traceID string, extraMsg string) {
	if user == "" {
		user = "system"
	}
	if cluster == "" {
		cluster = "-"
	}
	if traceID == "" {
		traceID = "-"
	}
	msg := fmt.Sprintf("[Task: %s] [Status: %s] [Duration: -] [Cluster: %s] [User: %s] [TraceID: %s] %s",
		taskName, TaskStatusInProgress, cluster, user, traceID, extraMsg)

	recorder.Event(object, corev1.EventTypeNormal, EventReasonExecutionStarted, msg)
}

// ReportTaskFinished 发射任务结束事件 (旧版本，不支持 Label)
func ReportTaskFinished(recorder record.EventRecorder, object runtime.Object, taskName string, cluster string, status string, startTime, endTime *metav1.Time, user string, traceID string, extraMsg string) {
	if user == "" {
		user = "system"
	}
	if cluster == "" {
		cluster = "-"
	}
	if traceID == "" {
		traceID = "-"
	}
	duration := CalculateDuration(startTime, endTime)

	msg := fmt.Sprintf("[Task: %s] [Status: %s] [Duration: %s] [Cluster: %s] [User: %s] [TraceID: %s] %s",
		taskName, status, duration, cluster, user, traceID, extraMsg)

	eventType := corev1.EventTypeNormal
	if status == TaskStatusFailed {
		eventType = corev1.EventTypeWarning
	}

	recorder.Event(object, eventType, EventReasonExecutionFinished, msg)
}

// IsTerminalPhase 判断是否为终态
func IsTerminalPhase(phase string) bool {
	switch phase {
	case "Completed", "Failed", "PartiallyFailed", "Canceled", "Succeeded":
		return true
	default:
		return false
	}
}
