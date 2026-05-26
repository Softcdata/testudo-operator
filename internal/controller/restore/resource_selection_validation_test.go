package restore

import (
	"strings"
	"testing"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

func TestValidateRestoreResourceSelectionAtSubmission(t *testing.T) {
	t.Parallel()

	includeClusterResources := true
	tests := []struct {
		name        string
		policy      *disasterv1.RestoreResourceSelectionPolicy
		wantErr     bool
		errContains string
	}{
		{
			name:    "nil policy",
			policy:  nil,
			wantErr: false,
		},
		{
			name: "old conflict",
			policy: &disasterv1.RestoreResourceSelectionPolicy{
				IncludedResources: []string{"deployments.apps"},
				ExcludedResources: []string{"deployments.apps"},
			},
			wantErr:     true,
			errContains: "includedResources",
		},
		{
			name: "includeClusterResources true skips scoped conflict",
			policy: &disasterv1.RestoreResourceSelectionPolicy{
				IncludeClusterResources:          &includeClusterResources,
				IncludedResources:                []string{"deployments.apps"},
				ExcludedResources:                []string{"secrets"},
				IncludedNamespaceScopedResources: []string{"services"},
				ExcludedNamespaceScopedResources: []string{"services"},
			},
			wantErr: false,
		},
		{
			name: "scoped namespace conflict",
			policy: &disasterv1.RestoreResourceSelectionPolicy{
				IncludedNamespaceScopedResources: []string{"services"},
				ExcludedNamespaceScopedResources: []string{"services"},
			},
			wantErr:     true,
			errContains: "includedNamespaceScopedResources",
		},
		{
			name: "scoped wildcard conflict",
			policy: &disasterv1.RestoreResourceSelectionPolicy{
				IncludedClusterScopedResources: []string{"*"},
				ExcludedClusterScopedResources: []string{"clusterroles"},
			},
			wantErr:     true,
			errContains: "includedClusterScopedResources",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRestoreResourceSelectionAtSubmission(tt.policy)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tt.errContains != "" && (err == nil || !strings.Contains(err.Error(), tt.errContains)) {
				t.Fatalf("expected error contains %q, got %v", tt.errContains, err)
			}
		})
	}
}
