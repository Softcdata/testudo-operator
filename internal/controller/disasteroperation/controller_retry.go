package disasteroperation

import (
	"time"

	runtimecfg "github.com/softcdata/testudo-operator/internal/controller/runtimeconfig"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

// shouldRetry 检查是否应该重试
func (r *DisasterOperationReconciler) shouldRetry(operation *disasterv1.DisasterOperation) bool {
	if operation.Spec.RetryPolicy == nil {
		return false
	}
	// MaxRetries=0 表示不重试
	if operation.Spec.RetryPolicy.MaxRetries <= 0 {
		return false
	}
	// 检查次数
	if operation.Status.RetryCount >= operation.Spec.RetryPolicy.MaxRetries {
		return false
	}
	return true
}

func (r *DisasterOperationReconciler) retryWaitDuration(operation *disasterv1.DisasterOperation) time.Duration {
	if operation == nil || operation.Spec.RetryPolicy == nil || operation.Spec.RetryPolicy.RetryIntervalSeconds <= 0 {
		return runtimecfg.SnapshotCurrent().OperationRuntime.DefaultRetryInterval
	}
	return time.Duration(operation.Spec.RetryPolicy.RetryIntervalSeconds) * time.Second
}

func (r *DisasterOperationReconciler) syncRetryWaitRemaining(operation *disasterv1.DisasterOperation, now time.Time) (time.Duration, bool) {
	if operation == nil || operation.Status.NextRetryTime == nil {
		return 0, false
	}
	if now.IsZero() {
		now = time.Now()
	}
	remaining := operation.Status.NextRetryTime.Time.Sub(now)
	if remaining <= 0 {
		return 0, false
	}
	return remaining, true
}
