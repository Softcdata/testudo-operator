package disasteroperation

import (
	"reflect"
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
)

func TestResolveDrillDataRestoreHooks(t *testing.T) {
	instanceHooks := &velerov1.RestoreHooks{Resources: []velerov1.RestoreResourceHookSpec{{Name: "instance"}}}
	drillHooks := &velerov1.RestoreHooks{Resources: []velerov1.RestoreResourceHookSpec{{Name: "drill"}}}

	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			VeleroHooks: &disasterv1.DisasterVeleroHooks{DataRestore: instanceHooks},
		},
	}

	if got := resolveDrillDataRestoreHooks(instance, nil); !reflect.DeepEqual(got, instanceHooks) {
		t.Fatalf("expected instance hooks, got %#v", got)
	}
	if got := resolveDrillDataRestoreHooks(instance, &disasterv1.DrillConfig{VeleroHooks: &disasterv1.DisasterVeleroHooks{DataRestore: drillHooks}}); !reflect.DeepEqual(got, drillHooks) {
		t.Fatalf("expected drill hooks, got %#v", got)
	}
	if got := resolveDrillDataRestoreHooks(instance, &disasterv1.DrillConfig{VeleroHooks: &disasterv1.DisasterVeleroHooks{}}); got != nil {
		t.Fatalf("expected drill empty hooks to clear inherited hooks, got %#v", got)
	}
}
