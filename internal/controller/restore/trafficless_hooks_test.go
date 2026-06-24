package restore

import (
	"reflect"
	"testing"
	"time"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPrepareTrafficlessDataRestoreHooks_RewritesExecHookSelector(t *testing.T) {
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "hook-di"}}
	hooks := &velerov1.RestoreHooks{
		Resources: []velerov1.RestoreResourceHookSpec{{
			Name:               "post",
			IncludedNamespaces: []string{"app"},
			IncludedResources:  []string{"pods"},
			LabelSelector:      selector,
			PostHooks: []velerov1.RestoreResourceHook{{
				Exec: &velerov1.ExecRestoreHook{
					Container:    "app",
					Command:      []string{"/bin/hook", "after"},
					OnError:      velerov1.HookErrorModeFail,
					ExecTimeout:  metav1.Duration{Duration: time.Minute},
					WaitTimeout:  metav1.Duration{Duration: 2 * time.Minute},
					WaitForReady: boolPtr(true),
				},
			}},
		}},
	}

	rewritten, markerRules := PrepareTrafficlessDataRestoreHooks(hooks, []string{"app"}, nil)
	if len(markerRules) != 1 {
		t.Fatalf("expected one marker rule, got %#v", markerRules)
	}
	markerKey := DataRestoreHookMarkerLabelKey(0)
	wantSelector := &metav1.LabelSelector{MatchLabels: map[string]string{markerKey: "true"}}
	if !reflect.DeepEqual(rewritten.Resources[0].LabelSelector, wantSelector) {
		t.Fatalf("expected marker selector %#v, got %#v", wantSelector, rewritten.Resources[0].LabelSelector)
	}
	if !reflect.DeepEqual(rewritten.Resources[0].PostHooks, hooks.Resources[0].PostHooks) {
		t.Fatalf("expected exec hook parameters preserved, got %#v", rewritten.Resources[0].PostHooks)
	}
	assertMarkerRule(t, markerRules[0], selector, []string{"app"}, "/metadata/labels/testudo.softcdata.com~1data-restore-hook-0")
}

func TestPrepareTrafficlessDataRestoreHooks_NamespaceMappingUsesBackupAndTargetPhases(t *testing.T) {
	hooks := &velerov1.RestoreHooks{
		Resources: []velerov1.RestoreResourceHookSpec{{
			Name:               "post",
			IncludedNamespaces: []string{"app-ns"},
			IncludedResources:  []string{"pods"},
			LabelSelector:      &metav1.LabelSelector{MatchLabels: map[string]string{"app": "hook-di"}},
			PostHooks: []velerov1.RestoreResourceHook{{
				Exec: &velerov1.ExecRestoreHook{Command: []string{"/bin/hook", "after"}},
			}},
		}},
	}

	rewritten, markerRules := PrepareTrafficlessDataRestoreHooks(
		hooks,
		[]string{"app-ns"},
		map[string]string{"app-ns": "drill-ns"},
	)
	if len(markerRules) != 1 {
		t.Fatalf("expected one marker rule, got %#v", markerRules)
	}
	if !reflect.DeepEqual(markerRules[0].Conditions.Namespaces, []string{"app-ns"}) {
		t.Fatalf("expected marker rule to match backup namespace app-ns, got %#v", markerRules[0].Conditions.Namespaces)
	}
	if !reflect.DeepEqual(rewritten.Resources[0].IncludedNamespaces, []string{"drill-ns"}) {
		t.Fatalf("expected exec hook to match target namespace drill-ns, got %#v", rewritten.Resources[0].IncludedNamespaces)
	}
}

func TestPrepareTrafficlessDataRestoreHooks_ExcludedNamespacesUseRestoreScopeDifference(t *testing.T) {
	hooks := &velerov1.RestoreHooks{
		Resources: []velerov1.RestoreResourceHookSpec{{
			Name:               "post",
			ExcludedNamespaces: []string{"skip"},
			IncludedResources:  []string{"pods"},
			PostHooks: []velerov1.RestoreResourceHook{{
				Exec: &velerov1.ExecRestoreHook{Command: []string{"/bin/hook", "after"}},
			}},
		}},
	}

	_, markerRules := PrepareTrafficlessDataRestoreHooks(hooks, []string{"app", "skip"}, nil)
	if len(markerRules) != 1 {
		t.Fatalf("expected one marker rule, got %#v", markerRules)
	}
	if !reflect.DeepEqual(markerRules[0].Conditions.Namespaces, []string{"app"}) {
		t.Fatalf("expected excluded namespace removed from marker rule, got %#v", markerRules[0].Conditions.Namespaces)
	}
}

func TestPrepareTrafficlessDataRestoreHooks_SkipsNonPodHookResources(t *testing.T) {
	hooks := &velerov1.RestoreHooks{
		Resources: []velerov1.RestoreResourceHookSpec{{
			Name:              "service",
			IncludedResources: []string{"services"},
			PostHooks: []velerov1.RestoreResourceHook{{
				Exec: &velerov1.ExecRestoreHook{Command: []string{"/bin/hook", "after"}},
			}},
		}},
	}

	rewritten, markerRules := PrepareTrafficlessDataRestoreHooks(hooks, []string{"app"}, nil)
	if len(markerRules) != 0 {
		t.Fatalf("expected non-pod hook not to create marker rules, got %#v", markerRules)
	}
	if !reflect.DeepEqual(rewritten, hooks) {
		t.Fatalf("expected non-pod hook unchanged, got %#v", rewritten)
	}
}

func assertMarkerRule(
	t *testing.T,
	rule disasterv1.ResourceModifierRule,
	selector *metav1.LabelSelector,
	namespaces []string,
	patchPath string,
) {
	t.Helper()
	if rule.Conditions.GroupResource != "pods" {
		t.Fatalf("expected marker rule for pods, got %s", rule.Conditions.GroupResource)
	}
	if !reflect.DeepEqual(rule.Conditions.LabelSelector, selector) {
		t.Fatalf("expected marker rule selector %#v, got %#v", selector, rule.Conditions.LabelSelector)
	}
	if !reflect.DeepEqual(rule.Conditions.Namespaces, namespaces) {
		t.Fatalf("expected marker rule namespaces %#v, got %#v", namespaces, rule.Conditions.Namespaces)
	}
	if len(rule.Patches) != 1 {
		t.Fatalf("expected one marker patch, got %#v", rule.Patches)
	}
	if rule.Patches[0].Operation != "add" || rule.Patches[0].Path != patchPath || rule.Patches[0].Value != `"true"` {
		t.Fatalf("unexpected marker patch: %#v", rule.Patches[0])
	}
}
