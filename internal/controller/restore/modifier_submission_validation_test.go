package restore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

type fakeModifierRuleResourceLocator struct {
	byGroupResource map[string][]unstructured.Unstructured
}

func (f *fakeModifierRuleResourceLocator) ListMatchingResources(
	_ context.Context,
	conditions disasterv1.Conditions,
	_ []string,
) ([]unstructured.Unstructured, error) {
	groupResource := strings.TrimSpace(conditions.GroupResource)
	items := f.byGroupResource[groupResource]
	out := make([]unstructured.Unstructured, 0, len(items))
	for _, item := range items {
		out = append(out, *item.DeepCopy())
	}
	return out, nil
}

func TestValidateModifierRulesAtSubmission_PathEscapeAndArrayIndex(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{
		{
			ID:   "anno",
			Mode: disasterv1.RestoreModifierModeReversible,
			Conditions: disasterv1.Conditions{
				GroupResource: "deployments.apps",
			},
			Pair: &disasterv1.RestoreModifierPair{
				Path:        "/metadata/annotations/testudo.softcdata.com~1source",
				SourceValue: "b",
				TargetValue: "a",
			},
		},
		{
			ID:   "arr",
			Mode: disasterv1.RestoreModifierModeReversible,
			Conditions: disasterv1.Conditions{
				GroupResource: "services",
			},
			Pair: &disasterv1.RestoreModifierPair{
				Path:        "/spec/ports/0/nodePort",
				SourceValue: "30080",
				TargetValue: "32080",
			},
		},
	})
	instance.Spec.Namespaces = []string{"prod"}

	locator := &fakeModifierRuleResourceLocator{
		byGroupResource: map[string][]unstructured.Unstructured{
			"deployments.apps": {
				{
					Object: map[string]any{
						"metadata": map[string]any{
							"name":      "order-api",
							"namespace": "prod",
							"annotations": map[string]any{
								"testudo.softcdata.com/source": "cluster-a",
							},
						},
					},
				},
			},
			"services": {
				{
					Object: map[string]any{
						"metadata": map[string]any{
							"name":      "core-gateway",
							"namespace": "prod",
						},
						"spec": map[string]any{
							"ports": []any{
								map[string]any{"nodePort": int64(30080)},
							},
						},
					},
				},
			},
		},
	}

	err := ValidateModifierRulesAtSubmission(
		context.Background(),
		instance,
		"cluster-a",
		"cluster-b",
		locator,
	)
	if err != nil {
		t.Fatalf("expected submission validation success, got: %v", err)
	}
}

func TestValidateModifierRulesAtSubmission_AllowsMetadataStringFieldsThatNeedVeleroStringPreservation(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{{
		ID:   "anno-number-string",
		Mode: disasterv1.RestoreModifierModeReversible,
		Conditions: disasterv1.Conditions{
			GroupResource: "deployments.apps",
		},
		Pair: &disasterv1.RestoreModifierPair{
			Path:        "/metadata/annotations/testudo.softcdata.com~1site-role",
			SourceValue: "1",
			TargetValue: "2",
		},
	}})

	locator := &fakeModifierRuleResourceLocator{
		byGroupResource: map[string][]unstructured.Unstructured{
			"deployments.apps": {{
				Object: map[string]any{
					"metadata": map[string]any{
						"name":      "order-api",
						"namespace": "prod",
						"annotations": map[string]any{
							"testudo.softcdata.com/site-role": "1",
						},
					},
				},
			}},
		},
	}

	err := ValidateModifierRulesAtSubmission(
		context.Background(),
		instance,
		"cluster-a",
		"cluster-b",
		locator,
	)
	if err != nil {
		t.Fatalf("expected submission validation success, got: %v", err)
	}
}

func TestValidateModifierRulesAtSubmission_StringFieldNumberLiteralRejected(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{{
		ID:   "env-string-number",
		Mode: disasterv1.RestoreModifierModeReversible,
		Conditions: disasterv1.Conditions{
			GroupResource: "deployments.apps",
		},
		Pair: &disasterv1.RestoreModifierPair{
			Path:        "/spec/template/spec/containers/0/env/0/value",
			SourceValue: "1",
			TargetValue: "2",
		},
	}})

	locator := &fakeModifierRuleResourceLocator{
		byGroupResource: map[string][]unstructured.Unstructured{
			"deployments.apps": {{
				Object: map[string]any{
					"metadata": map[string]any{
						"name":      "order-api",
						"namespace": "prod",
					},
					"spec": map[string]any{
						"template": map[string]any{
							"spec": map[string]any{
								"containers": []any{
									map[string]any{
										"env": []any{
											map[string]any{"value": "cluster-a"},
										},
									},
								},
							},
						},
					},
				},
			}},
		},
	}

	err := ValidateModifierRulesAtSubmission(
		context.Background(),
		instance,
		"cluster-a",
		"cluster-b",
		locator,
	)
	if err == nil {
		t.Fatalf("expected string field type rejection, got nil")
	}
	if !strings.Contains(err.Error(), ModifierErrorRuleRejected) {
		t.Fatalf("expected error contains %s, got %v", ModifierErrorRuleRejected, err)
	}
	if !strings.Contains(err.Error(), "would be applied as number but live field type is string") {
		t.Fatalf("expected type mismatch detail, got %v", err)
	}
}

func TestValidateModifierRulesAtSubmission_ArrayOutOfBoundsRejected(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{
		{
			ID:   "oob",
			Mode: disasterv1.RestoreModifierModeReversible,
			Conditions: disasterv1.Conditions{
				GroupResource: "services",
			},
			Pair: &disasterv1.RestoreModifierPair{
				Path:        "/spec/ports/10/nodePort",
				SourceValue: "30080",
				TargetValue: "32080",
			},
		},
	})

	locator := &fakeModifierRuleResourceLocator{
		byGroupResource: map[string][]unstructured.Unstructured{
			"services": {
				{
					Object: map[string]any{
						"metadata": map[string]any{"name": "svc-1", "namespace": "prod"},
						"spec": map[string]any{
							"ports": []any{
								map[string]any{"nodePort": int64(30080)},
							},
						},
					},
				},
			},
		},
	}

	err := ValidateModifierRulesAtSubmission(
		context.Background(),
		instance,
		"cluster-a",
		"cluster-b",
		locator,
	)
	if err == nil {
		t.Fatalf("expected submission validation to fail for array out-of-bounds")
	}
	if !strings.Contains(err.Error(), ModifierErrorRuleRejected) {
		t.Fatalf("expected error contains %s, got %v", ModifierErrorRuleRejected, err)
	}
	if !strings.Contains(err.Error(), "array index 10 out of bounds") {
		t.Fatalf("expected array out-of-bounds detail, got %v", err)
	}
}

func TestValidateModifierRulesAtSubmission_PathNotFoundRejected(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{
		{
			ID:   "missing-path",
			Mode: disasterv1.RestoreModifierModeVeleroNative,
			Conditions: disasterv1.Conditions{
				GroupResource: "deployments.apps",
			},
			VeleroRule: &disasterv1.RestoreModifierVeleroRule{
				Patches: []disasterv1.JSONPatch{{
					Operation: "replace",
					Path:      "/metadata/annotations/patched-by",
					Value:     "platform",
				}},
			},
		},
	})

	locator := &fakeModifierRuleResourceLocator{
		byGroupResource: map[string][]unstructured.Unstructured{
			"deployments.apps": {
				{
					Object: map[string]any{
						"metadata": map[string]any{
							"name":      "order-api",
							"namespace": "prod",
							"annotations": map[string]any{
								"owner": "team-a",
							},
						},
					},
				},
			},
		},
	}

	err := ValidateModifierRulesAtSubmission(
		context.Background(),
		instance,
		"cluster-a",
		"cluster-b",
		locator,
	)
	if err == nil {
		t.Fatalf("expected path-not-found rejection, got nil")
	}
	if !strings.Contains(err.Error(), ModifierErrorRuleRejected) {
		t.Fatalf("expected error contains %s, got %v", ModifierErrorRuleRejected, err)
	}
	if !strings.Contains(err.Error(), `path segment "patched-by" not found`) {
		t.Fatalf("expected path-not-found detail, got %v", err)
	}
}

func TestValidateModifierRulesAtSubmission_AddMissingFinalMapKeyAllowed(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{
		{
			ID:   "add-label",
			Mode: disasterv1.RestoreModifierModeVeleroNative,
			Conditions: disasterv1.Conditions{
				GroupResource: "deployments.apps",
			},
			VeleroRule: &disasterv1.RestoreModifierVeleroRule{
				Patches: []disasterv1.JSONPatch{{
					Operation: "add",
					Path:      "/metadata/labels/type",
					Value:     "nginx",
				}},
			},
		},
	})

	locator := &fakeModifierRuleResourceLocator{
		byGroupResource: map[string][]unstructured.Unstructured{
			"deployments.apps": {
				{
					Object: map[string]any{
						"metadata": map[string]any{
							"name":      "order-api",
							"namespace": "prod",
							"labels": map[string]any{
								"app": "order-api",
							},
						},
					},
				},
			},
		},
	}

	err := ValidateModifierRulesAtSubmission(
		context.Background(),
		instance,
		"cluster-a",
		"cluster-b",
		locator,
	)
	if err != nil {
		t.Fatalf("expected missing final map key add to pass, got %v", err)
	}
}

func TestValidateModifierRulesAtSubmission_AddMissingIntermediateMapKeyRejected(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{
		{
			ID:   "add-missing-parent",
			Mode: disasterv1.RestoreModifierModeVeleroNative,
			Conditions: disasterv1.Conditions{
				GroupResource: "deployments.apps",
			},
			VeleroRule: &disasterv1.RestoreModifierVeleroRule{
				Patches: []disasterv1.JSONPatch{{
					Operation: "add",
					Path:      "/metadata/not-exists/type",
					Value:     "nginx",
				}},
			},
		},
	})

	locator := &fakeModifierRuleResourceLocator{
		byGroupResource: map[string][]unstructured.Unstructured{
			"deployments.apps": {
				{
					Object: map[string]any{
						"metadata": map[string]any{
							"name":      "order-api",
							"namespace": "prod",
							"labels": map[string]any{
								"app": "order-api",
							},
						},
					},
				},
			},
		},
	}

	err := ValidateModifierRulesAtSubmission(
		context.Background(),
		instance,
		"cluster-a",
		"cluster-b",
		locator,
	)
	if err == nil {
		t.Fatalf("expected add with missing intermediate map key to fail")
	}
	if !strings.Contains(err.Error(), ModifierErrorRuleRejected) {
		t.Fatalf("expected error contains %s, got %v", ModifierErrorRuleRejected, err)
	}
	if !strings.Contains(err.Error(), `path segment "not-exists" not found`) {
		t.Fatalf("expected missing intermediate path detail, got %v", err)
	}
}

func TestValidateModifierRulesAtSubmission_ZeroMatchRejected(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{
		{
			ID:   "no-hit",
			Mode: disasterv1.RestoreModifierModeVeleroNative,
			Conditions: disasterv1.Conditions{
				GroupResource: "deployments.apps",
			},
			VeleroRule: &disasterv1.RestoreModifierVeleroRule{
				Patches: []disasterv1.JSONPatch{{
					Operation: "add",
					Path:      "/metadata/annotations/patched-by",
					Value:     "platform",
				}},
			},
		},
	})

	locator := &fakeModifierRuleResourceLocator{
		byGroupResource: map[string][]unstructured.Unstructured{
			"deployments.apps": {},
		},
	}

	err := ValidateModifierRulesAtSubmission(
		context.Background(),
		instance,
		"cluster-a",
		"cluster-b",
		locator,
	)
	if err == nil {
		t.Fatalf("expected zero-match rejection, got nil")
	}
	if !strings.Contains(err.Error(), "matched zero resources") {
		t.Fatalf("expected zero-match detail, got %v", err)
	}
}

func TestValidateModifierRulesAtSubmission_CompileChecksReverseDirection(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{
		{
			ID:   "missing-reverse",
			Mode: disasterv1.RestoreModifierModeReversible,
			Conditions: disasterv1.Conditions{
				GroupResource: "services",
			},
			Pair: &disasterv1.RestoreModifierPair{
				Path:        "/spec/ports/0/nodePort",
				TargetValue: "32080",
			},
		},
	})

	locator := &fakeModifierRuleResourceLocator{
		byGroupResource: map[string][]unstructured.Unstructured{
			"services": {
				{
					Object: map[string]any{
						"metadata": map[string]any{"name": "svc-1", "namespace": "prod"},
						"spec": map[string]any{
							"ports": []any{
								map[string]any{"nodePort": int64(30080)},
							},
						},
					},
				},
			},
		},
	}

	err := ValidateModifierRulesAtSubmission(
		context.Background(),
		instance,
		"cluster-a",
		"cluster-b",
		locator,
	)
	if err == nil {
		t.Fatalf("expected reverse-direction compile failure, got nil")
	}
	if !strings.Contains(err.Error(), ModifierErrorRuleNotReversible) {
		t.Fatalf("expected %s, got %v", ModifierErrorRuleNotReversible, err)
	}
}

func TestValidateModifierRulesAtSubmission_RuleNamespaceOutsideInstanceRejected(t *testing.T) {
	t.Parallel()

	instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{{
		ID:   "scope-outside",
		Mode: disasterv1.RestoreModifierModeReversible,
		Conditions: disasterv1.Conditions{
			GroupResource: "deployments.apps",
			Namespaces:    []string{"outside-ns"},
		},
		Pair: &disasterv1.RestoreModifierPair{
			Path:        "/metadata/annotations/patched-by",
			SourceValue: "rev",
			TargetValue: "fwd",
		},
	}})
	instance.Spec.Namespaces = []string{"allowed-ns"}

	locator := &fakeModifierRuleResourceLocator{}
	err := ValidateModifierRulesAtSubmission(
		context.Background(),
		instance,
		"cluster-a",
		"cluster-b",
		locator,
	)
	if err == nil {
		t.Fatalf("expected namespace scope rejection, got nil")
	}
	if !strings.Contains(err.Error(), ModifierErrorRuleRejected) {
		t.Fatalf("expected error contains %s, got %v", ModifierErrorRuleRejected, err)
	}
	if !strings.Contains(err.Error(), "outside instance namespaces") {
		t.Fatalf("expected outside scope detail, got %v", err)
	}
}

func TestValidateModifierRulesAtSubmission_LegacyTransformInputRejected(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "legacy map",
			raw:  `{"id":"legacy-map","mode":"reversible","conditions":{"groupResource":"services"},"transform":{"type":"map","path":"/spec/ports/0/nodePort","mapping":{"30080":"32080"}}}`,
		},
		{
			name: "legacy template",
			raw:  `{"id":"legacy-template","mode":"reversible","conditions":{"groupResource":"deployments.apps"},"transform":{"type":"template","path":"/spec/template/spec/containers/0/env/0/value","valueTemplate":"mysql.{{ .TargetCluster }}.svc"}}`,
		},
	}

	locator := &fakeModifierRuleResourceLocator{
		byGroupResource: map[string][]unstructured.Unstructured{
			"services": {
				{
					Object: map[string]any{
						"metadata": map[string]any{"name": "svc-1", "namespace": "prod"},
						"spec": map[string]any{
							"ports": []any{
								map[string]any{"nodePort": int64(30080)},
							},
						},
					},
				},
			},
			"deployments.apps": {
				{
					Object: map[string]any{
						"metadata": map[string]any{"name": "order-api", "namespace": "prod"},
						"spec": map[string]any{
							"template": map[string]any{
								"spec": map[string]any{
									"containers": []any{
										map[string]any{
											"env": []any{
												map[string]any{"value": "mysql.cluster-a.svc"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var rule disasterv1.RestoreModifierRule
			if err := json.Unmarshal([]byte(tc.raw), &rule); err != nil {
				t.Fatalf("unexpected unmarshal error: %v", err)
			}

			instance := baseInstanceWithPolicy(true, []disasterv1.RestoreModifierRule{rule})
			err := ValidateModifierRulesAtSubmission(
				context.Background(),
				instance,
				"cluster-a",
				"cluster-b",
				locator,
			)
			if err == nil {
				t.Fatalf("expected legacy transform rejection")
			}
			if !strings.Contains(err.Error(), "pair canonical form") {
				t.Fatalf("expected pair canonical guidance, got %v", err)
			}
		})
	}
}
