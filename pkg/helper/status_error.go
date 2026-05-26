package helper

import (
	"reflect"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SetStatusError sets status.reason/status.message if the fields exist on status.
func SetStatusError(status any, reason, message string) {
	setStatusStringField(status, "Reason", reason)
	setStatusStringField(status, "Message", message)
}

// ClearStatusError clears status.reason/status.message if the fields exist on status.
func ClearStatusError(status any) {
	setStatusStringField(status, "Reason", "")
	setStatusStringField(status, "Message", "")
}

// SetConditionError writes a failure condition with a stable reason and readable message.
func SetConditionError(conditions *[]metav1.Condition, conditionType, reason, message string) {
	if conditions == nil {
		return
	}
	if conditionType == "" {
		conditionType = "Error"
	}
	apimeta.SetStatusCondition(conditions, metav1.Condition{
		Type:               conditionType,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})
}

func setStatusStringField(status any, fieldName, value string) {
	if status == nil {
		return
	}
	v := reflect.ValueOf(status)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}
	elem := v.Elem()
	if elem.Kind() != reflect.Struct {
		return
	}
	field := elem.FieldByName(fieldName)
	if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.String {
		return
	}
	field.SetString(value)
}
