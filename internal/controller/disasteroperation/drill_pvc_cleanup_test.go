package disasteroperation

import (
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

func TestShouldDrillCleanupPVCVolumeName(t *testing.T) {
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app-ns"},
		},
	}

	cases := []struct {
		name            string
		instance        *disasterv1.DisasterInstance
		namespaceMapper map[string]string
		want            bool
	}{
		{
			name:            "nil instance",
			instance:        nil,
			namespaceMapper: map[string]string{"app-ns": "drill-ns"},
			want:            false,
		},
		{
			name:            "empty mapping",
			instance:        instance,
			namespaceMapper: nil,
			want:            false,
		},
		{
			name:            "same namespace mapping",
			instance:        instance,
			namespaceMapper: map[string]string{"app-ns": "app-ns"},
			want:            false,
		},
		{
			name:            "new target namespace mapping",
			instance:        instance,
			namespaceMapper: map[string]string{"app-ns": "drill-ns"},
			want:            true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldDrillCleanupPVCVolumeName(tc.instance, tc.namespaceMapper)
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestDrillPVCVolumeCleanupNamespaces(t *testing.T) {
	got := drillPVCVolumeCleanupNamespaces(
		[]string{"app-ns", " db-ns "},
		map[string]string{
			"app-ns": "drill-app-ns",
			"db-ns":  "drill-db-ns",
			"other":  "drill-app-ns", // duplicate destination should be deduped
		},
	)

	wantSet := map[string]struct{}{
		"app-ns":       {},
		"db-ns":        {},
		"drill-app-ns": {},
		"drill-db-ns":  {},
	}
	if len(got) != len(wantSet) {
		t.Fatalf("unexpected namespace count: got=%d want=%d (%#v)", len(got), len(wantSet), got)
	}
	for _, ns := range got {
		if _, ok := wantSet[ns]; !ok {
			t.Fatalf("unexpected namespace in result: %s (%#v)", ns, got)
		}
		delete(wantSet, ns)
	}
	if len(wantSet) != 0 {
		t.Fatalf("missing namespaces in result: %#v", wantSet)
	}
}

func TestBuildDrillPVCVolumeNameCleanupRule(t *testing.T) {
	instance := &disasterv1.DisasterInstance{
		Spec: disasterv1.DisasterInstanceSpec{
			Namespaces: []string{"app-ns"},
		},
	}

	rule, ok := buildDrillPVCVolumeNameCleanupRule(instance, map[string]string{"app-ns": "drill-app-ns"})
	if !ok {
		t.Fatalf("expected cleanup rule to be enabled")
	}
	if rule.Conditions.GroupResource != "persistentvolumeclaims" {
		t.Fatalf("expected pvc groupResource, got %s", rule.Conditions.GroupResource)
	}
	if len(rule.Patches) != 1 || rule.Patches[0].Operation != "add" || rule.Patches[0].Path != "/spec/volumeName" || rule.Patches[0].Value != "" {
		t.Fatalf("unexpected patches: %#v", rule.Patches)
	}
	contains := map[string]bool{}
	for _, ns := range rule.Conditions.Namespaces {
		contains[ns] = true
	}
	if !contains["app-ns"] || !contains["drill-app-ns"] {
		t.Fatalf("expected rule namespaces include source and mapped namespace, got %#v", rule.Conditions.Namespaces)
	}

	if _, ok := buildDrillPVCVolumeNameCleanupRule(instance, map[string]string{"app-ns": "app-ns"}); ok {
		t.Fatalf("did not expect cleanup rule for same-namespace mapping")
	}
}
