package restore

import (
	"fmt"
	"strings"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

const (
	// DataRestoreHookMarkerLabelPrefix is used on trafficless restored pods so
	// Velero exec restore hooks can match after business labels are removed.
	DataRestoreHookMarkerLabelPrefix = "testudo.softcdata.com/data-restore-hook-"

	dataRestoreHookMarkerLabelValue = "true"
	podResourceName                 = "pods"
)

// DataRestoreHookMarkerLabelKey returns the marker label key for one restore hook resource.
func DataRestoreHookMarkerLabelKey(index int) string {
	return fmt.Sprintf("%s%d", DataRestoreHookMarkerLabelPrefix, index)
}

// PrepareTrafficlessDataRestoreHooks rewrites exec restore hook selectors so they still
// match pods after trafficless ResourceModifier rules remove business labels.
func PrepareTrafficlessDataRestoreHooks(
	hooks *velerov1.RestoreHooks,
	restoreIncludedNamespaces []string,
	namespaceMapping map[string]string,
) (*velerov1.RestoreHooks, []disasterv1.ResourceModifierRule) {
	if hooks == nil {
		return nil, nil
	}
	rewritten := hooks.DeepCopy()
	if rewritten == nil {
		return nil, nil
	}

	resources := make([]velerov1.RestoreResourceHookSpec, 0, len(rewritten.Resources)*2)
	markerRules := make([]disasterv1.ResourceModifierRule, 0)
	for idx := range rewritten.Resources {
		resource := rewritten.Resources[idx]
		if len(resource.PostHooks) == 0 || !restoreHookResourceTargetsPods(resource) {
			mapRestoreHookNamespacesToTarget(&resource, namespaceMapping)
			resources = append(resources, resource)
			continue
		}

		initHooks, execHooks, inertHooks := splitRestoreHooks(resource.PostHooks)
		if len(execHooks) == 0 {
			mapRestoreHookNamespacesToTarget(&resource, namespaceMapping)
			resources = append(resources, resource)
			continue
		}

		markerNamespaces, canExpressMarkerNamespaces := restoreHookMarkerNamespaces(resource, restoreIncludedNamespaces, namespaceMapping)
		if !canExpressMarkerNamespaces {
			mapRestoreHookNamespacesToTarget(&resource, namespaceMapping)
			resources = append(resources, resource)
			continue
		}

		if len(initHooks) > 0 || len(inertHooks) > 0 {
			initResource := *resource.DeepCopy()
			initResource.PostHooks = append(initHooks, inertHooks...)
			mapRestoreHookNamespacesToTarget(&initResource, namespaceMapping)
			resources = append(resources, initResource)
		}

		markerKey := DataRestoreHookMarkerLabelKey(idx)
		markerRules = append(markerRules, buildDataRestoreHookMarkerRule(resource, markerKey, markerNamespaces))

		execResource := *resource.DeepCopy()
		execResource.PostHooks = execHooks
		execResource.LabelSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{
				markerKey: dataRestoreHookMarkerLabelValue,
			},
		}
		mapRestoreHookNamespacesToTarget(&execResource, namespaceMapping)
		resources = append(resources, execResource)
	}
	rewritten.Resources = resources
	return rewritten, markerRules
}

func splitRestoreHooks(hooks []velerov1.RestoreResourceHook) (
	initHooks []velerov1.RestoreResourceHook,
	execHooks []velerov1.RestoreResourceHook,
	inertHooks []velerov1.RestoreResourceHook,
) {
	for idx := range hooks {
		hook := hooks[idx]
		if hook.Init == nil && hook.Exec == nil {
			inertHooks = append(inertHooks, hook)
			continue
		}
		if hook.Init != nil {
			initHook := *hook.DeepCopy()
			initHook.Exec = nil
			initHooks = append(initHooks, initHook)
		}
		if hook.Exec != nil {
			execHook := *hook.DeepCopy()
			execHook.Init = nil
			execHooks = append(execHooks, execHook)
		}
	}
	return initHooks, execHooks, inertHooks
}

func buildDataRestoreHookMarkerRule(
	resource velerov1.RestoreResourceHookSpec,
	markerKey string,
	namespaces []string,
) disasterv1.ResourceModifierRule {
	conditions := disasterv1.Conditions{
		GroupResource: podResourceName,
		Namespaces:    append([]string(nil), namespaces...),
	}
	if resource.LabelSelector != nil {
		conditions.LabelSelector = resource.LabelSelector.DeepCopy()
	}
	return disasterv1.ResourceModifierRule{
		Conditions: conditions,
		Patches: []disasterv1.JSONPatch{{
			Operation: "add",
			Path:      "/metadata/labels/" + escapeJSONPointerSegment(markerKey),
			Value:     `"` + dataRestoreHookMarkerLabelValue + `"`,
		}},
	}
}

func restoreHookResourceTargetsPods(resource velerov1.RestoreResourceHookSpec) bool {
	if stringSliceContainsPodResource(resource.ExcludedResources) {
		return false
	}
	return len(resource.IncludedResources) == 0 || stringSliceContainsPodResource(resource.IncludedResources)
}

func stringSliceContainsPodResource(resources []string) bool {
	for _, resource := range resources {
		switch strings.ToLower(strings.TrimSpace(resource)) {
		case "pod", podResourceName:
			return true
		}
	}
	return false
}

func mapRestoreHookNamespacesToTarget(resource *velerov1.RestoreResourceHookSpec, namespaceMapping map[string]string) {
	if resource == nil {
		return
	}
	resource.IncludedNamespaces = mapNamespaces(resource.IncludedNamespaces, func(namespace string) string {
		if target, ok := namespaceMapping[namespace]; ok {
			return target
		}
		return namespace
	})
	resource.ExcludedNamespaces = mapNamespaces(resource.ExcludedNamespaces, func(namespace string) string {
		if target, ok := namespaceMapping[namespace]; ok {
			return target
		}
		return namespace
	})
}

func restoreHookMarkerNamespaces(
	resource velerov1.RestoreResourceHookSpec,
	restoreIncludedNamespaces []string,
	namespaceMapping map[string]string,
) ([]string, bool) {
	included := resource.IncludedNamespaces
	if len(included) == 0 && len(restoreIncludedNamespaces) > 0 {
		included = restoreIncludedNamespaces
	}
	excluded := mapRestoreHookNamespacesToBackup(resource.ExcludedNamespaces, namespaceMapping)
	if len(included) == 0 {
		if len(excluded) > 0 {
			return nil, false
		}
		return nil, true
	}
	included = mapRestoreHookNamespacesToBackup(included, namespaceMapping)
	if len(excluded) == 0 {
		return included, true
	}
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, namespace := range excluded {
		excludedSet[namespace] = struct{}{}
	}
	out := make([]string, 0, len(included))
	for _, namespace := range included {
		if _, excluded := excludedSet[namespace]; excluded {
			continue
		}
		out = append(out, namespace)
	}
	return out, true
}

func mapRestoreHookNamespacesToBackup(namespaces []string, namespaceMapping map[string]string) []string {
	targetToSource := make(map[string]string, len(namespaceMapping))
	for source, target := range namespaceMapping {
		targetToSource[target] = source
	}
	return mapNamespaces(namespaces, func(namespace string) string {
		if _, ok := namespaceMapping[namespace]; ok {
			return namespace
		}
		if source, ok := targetToSource[namespace]; ok {
			return source
		}
		return namespace
	})
}

func mapNamespaces(namespaces []string, mapOne func(string) string) []string {
	if len(namespaces) == 0 {
		return nil
	}
	out := make([]string, 0, len(namespaces))
	seen := make(map[string]struct{}, len(namespaces))
	for _, namespace := range namespaces {
		mapped := mapOne(namespace)
		if _, ok := seen[mapped]; ok {
			continue
		}
		seen[mapped] = struct{}{}
		out = append(out, mapped)
	}
	return out
}

func escapeJSONPointerSegment(segment string) string {
	segment = strings.ReplaceAll(segment, "~", "~0")
	return strings.ReplaceAll(segment, "/", "~1")
}
