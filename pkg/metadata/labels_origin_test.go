package metadata

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func boolPtr(v bool) *bool {
	return &v
}

func TestResolveAppResourceOriginByOwnerRefs(t *testing.T) {
	tests := []struct {
		name      string
		ownerRefs []metav1.OwnerReference
		wantOri   string
		wantKind  string
		wantName  string
	}{
		{
			name:      "no owner refs defaults to user",
			ownerRefs: nil,
			wantOri:   AppResourceOriginUser,
			wantKind:  AppResourceOwnerKindUser,
			wantName:  "",
		},
		{
			name: "datasync owner",
			ownerRefs: []metav1.OwnerReference{
				{Kind: "DataSync", Name: "ds-demo", Controller: boolPtr(true)},
			},
			wantOri:  AppResourceOriginDisasterInstance,
			wantKind: AppResourceOwnerKindDataSync,
			wantName: "ds-demo",
		},
		{
			name: "resourcesync owner",
			ownerRefs: []metav1.OwnerReference{
				{Kind: "ResourceSync", Name: "rs-demo", Controller: boolPtr(true)},
			},
			wantOri:  AppResourceOriginDisasterInstance,
			wantKind: AppResourceOwnerKindResourceSync,
			wantName: "rs-demo",
		},
		{
			name: "non controller owner falls back to user",
			ownerRefs: []metav1.OwnerReference{
				{Kind: "DataSync", Name: "ds-demo", Controller: boolPtr(false)},
			},
			wantOri:  AppResourceOriginUser,
			wantKind: AppResourceOwnerKindUser,
			wantName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOri, gotKind, gotName := ResolveAppResourceOriginByOwnerRefs(tt.ownerRefs)
			if gotOri != tt.wantOri || gotKind != tt.wantKind || gotName != tt.wantName {
				t.Fatalf("ResolveAppResourceOriginByOwnerRefs() = (%s,%s,%s), want (%s,%s,%s)",
					gotOri, gotKind, gotName, tt.wantOri, tt.wantKind, tt.wantName)
			}
		})
	}
}

func TestEnsureAppResourceOriginLabels(t *testing.T) {
	labels := map[string]string{
		LabelAppResourceOrigin:    AppResourceOriginUser,
		LabelAppResourceOwnerKind: AppResourceOwnerKindUser,
		LabelAppResourceOwnerName: "stale-owner",
	}
	ownerRefs := []metav1.OwnerReference{
		{Kind: "DataSync", Name: "ds-a", Controller: boolPtr(true)},
	}

	changed := EnsureAppResourceOriginLabels(labels, ownerRefs)
	if !changed {
		t.Fatalf("EnsureAppResourceOriginLabels() expected changed=true")
	}
	if labels[LabelAppResourceOrigin] != AppResourceOriginDisasterInstance {
		t.Fatalf("origin = %s, want %s", labels[LabelAppResourceOrigin], AppResourceOriginDisasterInstance)
	}
	if labels[LabelAppResourceOwnerKind] != AppResourceOwnerKindDataSync {
		t.Fatalf("owner kind = %s, want %s", labels[LabelAppResourceOwnerKind], AppResourceOwnerKindDataSync)
	}
	if labels[LabelAppResourceOwnerName] != "ds-a" {
		t.Fatalf("owner name = %s, want ds-a", labels[LabelAppResourceOwnerName])
	}

	// fallback to user should remove owner-name
	changed = EnsureAppResourceOriginLabels(labels, nil)
	if !changed {
		t.Fatalf("EnsureAppResourceOriginLabels() expected changed=true on fallback")
	}
	if labels[LabelAppResourceOrigin] != AppResourceOriginUser {
		t.Fatalf("origin = %s, want %s", labels[LabelAppResourceOrigin], AppResourceOriginUser)
	}
	if labels[LabelAppResourceOwnerKind] != AppResourceOwnerKindUser {
		t.Fatalf("owner kind = %s, want %s", labels[LabelAppResourceOwnerKind], AppResourceOwnerKindUser)
	}
	if _, ok := labels[LabelAppResourceOwnerName]; ok {
		t.Fatalf("owner name should be removed for user origin")
	}
}
