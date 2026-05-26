package controller

import (
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type IgnoreStatusUpdatesPredicate struct {
	predicate.Funcs
}

// Update函数在对象更新时被调用。如果返回true，则触发Reconcile。
func (IgnoreStatusUpdatesPredicate) Update(e event.UpdateEvent) bool {
	// 如果旧对象为空，不处理
	if e.ObjectOld == nil {
		return false
	}
	if e.ObjectNew == nil {
		return false
	}

	// 比较新旧对象的Generation（Generation在Spec变化时会改变）
	// 如果Generation没变，说明只是Status或其他不影响Spec的元数据变了，忽略此次更新。
	if e.ObjectNew.GetGeneration() == e.ObjectOld.GetGeneration() {
		oldValue := e.ObjectOld.GetAnnotations()[AnnotationRefreshClusterStats]
		newValue := e.ObjectNew.GetAnnotations()[AnnotationRefreshClusterStats]
		return oldValue != newValue
	}

	// Generation改变了，说明Spec发生了变化，需要Reconcile。
	return true
}
