package imagemapping

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

// RegistryMapping is a resolved registry mapping pair.
type RegistryMapping struct {
	SourceAlias    string
	SourceRegistry string
	TargetAlias    string
	TargetRegistry string
}

// ResolveRegistryMappings validates and resolves image rewrite mappings into concrete registry mappings.
// Returns enabled=false when image rewrite is disabled or not applied to the current target.
func ResolveRegistryMappings(
	sourceCluster *disasterv1.Cluster,
	targetCluster *disasterv1.Cluster,
	imageRewrite *disasterv1.ImageRewriteConfig,
	applyTarget disasterv1.ImageRewriteApplyTarget,
) ([]RegistryMapping, disasterv1.ImageRewriteUnmatchedPolicy, bool, error) {
	policy := disasterv1.ImageRewriteUnmatchedPolicyFail
	if imageRewrite == nil {
		return nil, policy, false, nil
	}

	cfg := imageRewrite
	if cfg.UnmatchedPolicy == disasterv1.ImageRewriteUnmatchedPolicyKeep {
		policy = disasterv1.ImageRewriteUnmatchedPolicyKeep
	}

	if !cfg.Enabled {
		return nil, policy, false, nil
	}

	if len(cfg.ApplyTo) > 0 {
		allowed := false
		for _, target := range cfg.ApplyTo {
			if target == applyTarget {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, policy, false, nil
		}
	}

	if len(cfg.Mappings) == 0 {
		return nil, policy, false, fmt.Errorf("imageRewrite enabled but mappings is empty")
	}
	if sourceCluster == nil || targetCluster == nil {
		return nil, policy, false, fmt.Errorf("source/target cluster is nil")
	}

	sourceRegistryByAlias, err := buildRegistryAliasMap(sourceCluster.Spec.ImageSources, sourceCluster.Name)
	if err != nil {
		return nil, policy, false, err
	}
	targetRegistryByAlias, err := buildRegistryAliasMap(targetCluster.Spec.ImageSources, targetCluster.Name)
	if err != nil {
		return nil, policy, false, err
	}

	result := make([]RegistryMapping, 0, len(cfg.Mappings))
	seenSourceAlias := make(map[string]struct{}, len(cfg.Mappings))

	for _, item := range cfg.Mappings {
		sourceAlias := strings.TrimSpace(item.SourceImageSource)
		targetAlias := strings.TrimSpace(item.TargetImageSource)
		if sourceAlias == "" || targetAlias == "" {
			return nil, policy, false, fmt.Errorf("sourceImageSource/targetImageSource must not be empty")
		}
		if _, exists := seenSourceAlias[sourceAlias]; exists {
			return nil, policy, false, fmt.Errorf("duplicate sourceImageSource mapping: %s", sourceAlias)
		}
		seenSourceAlias[sourceAlias] = struct{}{}

		sourceRegistry, sourceExists := sourceRegistryByAlias[sourceAlias]
		targetRegistry, targetExists := targetRegistryByAlias[targetAlias]

		// When roles are switched after failover, mapping direction must follow current source/target.
		// If the configured direction is not valid in current runtime direction, try reverse pairing.
		if !sourceExists || !targetExists {
			reverseSourceRegistry, reverseSourceExists := sourceRegistryByAlias[targetAlias]
			reverseTargetRegistry, reverseTargetExists := targetRegistryByAlias[sourceAlias]
			if reverseSourceExists && reverseTargetExists {
				sourceAlias, targetAlias = targetAlias, sourceAlias
				sourceRegistry, targetRegistry = reverseSourceRegistry, reverseTargetRegistry
			} else {
				return nil, policy, false, fmt.Errorf(
					"image source alias pair not found for current clusters %s -> %s: %s -> %s",
					sourceCluster.Name,
					targetCluster.Name,
					item.SourceImageSource,
					item.TargetImageSource,
				)
			}
		}
		result = append(result, RegistryMapping{
			SourceAlias:    sourceAlias,
			SourceRegistry: sourceRegistry,
			TargetAlias:    targetAlias,
			TargetRegistry: targetRegistry,
		})
	}

	return result, policy, true, nil
}

// CollectWorkloads lists deployments/statefulsets from source cluster with namespace+selector filters.
func CollectWorkloads(
	ctx context.Context,
	remoteClient client.Client,
	namespaces []string,
	labelSelector *metav1.LabelSelector,
) ([]appsv1.Deployment, []appsv1.StatefulSet, error) {
	var selectorOpt client.ListOption
	if labelSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(labelSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid label selector: %w", err)
		}
		selectorOpt = client.MatchingLabelsSelector{Selector: selector}
	}

	deployments := make([]appsv1.Deployment, 0)
	statefulSets := make([]appsv1.StatefulSet, 0)

	for _, namespace := range namespaces {
		opts := []client.ListOption{client.InNamespace(namespace)}
		if selectorOpt != nil {
			opts = append(opts, selectorOpt)
		}

		deployList := &appsv1.DeploymentList{}
		if err := remoteClient.List(ctx, deployList, opts...); err != nil {
			return nil, nil, fmt.Errorf("list deployments in %s: %w", namespace, err)
		}
		deployments = append(deployments, deployList.Items...)

		stsList := &appsv1.StatefulSetList{}
		if err := remoteClient.List(ctx, stsList, opts...); err != nil {
			return nil, nil, fmt.Errorf("list statefulsets in %s: %w", namespace, err)
		}
		statefulSets = append(statefulSets, stsList.Items...)
	}

	return deployments, statefulSets, nil
}

// BuildRulesFromWorkloads builds image replace rules for workloads and returns unmatched image references.
func BuildRulesFromWorkloads(
	deployments []appsv1.Deployment,
	statefulSets []appsv1.StatefulSet,
	mappings []RegistryMapping,
	policy disasterv1.ImageRewriteUnmatchedPolicy,
) ([]disasterv1.ResourceModifierRule, []string) {
	rules := make([]disasterv1.ResourceModifierRule, 0)
	unmatchedSet := make(map[string]struct{})

	for i := range deployments {
		deploy := &deployments[i]
		patches := buildWorkloadImagePatches(
			deploy.Namespace,
			deploy.Name,
			deploy.Spec.Template.Spec.Containers,
			deploy.Spec.Template.Spec.InitContainers,
			mappings,
			policy,
			unmatchedSet,
		)
		if len(patches) > 0 {
			rules = append(rules, disasterv1.ResourceModifierRule{
				Conditions: disasterv1.Conditions{
					GroupResource:     "deployments.apps",
					ResourceNameRegex: exactResourceRegex(deploy.Name),
				},
				Patches: patches,
			})
		}
	}

	for i := range statefulSets {
		sts := &statefulSets[i]
		patches := buildWorkloadImagePatches(
			sts.Namespace,
			sts.Name,
			sts.Spec.Template.Spec.Containers,
			sts.Spec.Template.Spec.InitContainers,
			mappings,
			policy,
			unmatchedSet,
		)
		if len(patches) > 0 {
			rules = append(rules, disasterv1.ResourceModifierRule{
				Conditions: disasterv1.Conditions{
					GroupResource:     "statefulsets.apps",
					ResourceNameRegex: exactResourceRegex(sts.Name),
				},
				Patches: patches,
			})
		}
	}

	unmatched := make([]string, 0, len(unmatchedSet))
	for item := range unmatchedSet {
		unmatched = append(unmatched, item)
	}
	sort.Strings(unmatched)

	return rules, unmatched
}

// RewriteImage rewrites image registry prefix using the longest matching source registry.
func RewriteImage(image string, mappings []RegistryMapping) (string, bool) {
	current := strings.TrimSpace(image)
	if current == "" {
		return image, false
	}

	bestIndex := -1
	bestPrefixLen := -1
	for i, mapping := range mappings {
		source := normalizeRegistry(mapping.SourceRegistry)
		if source == "" {
			continue
		}
		if current == source || strings.HasPrefix(current, source+"/") {
			if len(source) > bestPrefixLen {
				bestPrefixLen = len(source)
				bestIndex = i
			}
		}
	}
	if bestIndex == -1 {
		return image, false
	}

	source := normalizeRegistry(mappings[bestIndex].SourceRegistry)
	target := normalizeRegistry(mappings[bestIndex].TargetRegistry)
	if current == source {
		return target, true
	}

	return target + strings.TrimPrefix(current, source), true
}

func buildRegistryAliasMap(sources []disasterv1.ImageSource, clusterName string) (map[string]string, error) {
	result := make(map[string]string, len(sources))
	for _, source := range sources {
		alias := strings.TrimSpace(source.Name)
		registry := normalizeRegistry(source.Registry)
		if alias == "" || registry == "" {
			continue
		}
		if _, exists := result[alias]; exists {
			return nil, fmt.Errorf("duplicate image source alias in cluster %s: %s", clusterName, alias)
		}
		result[alias] = registry
	}
	return result, nil
}

func buildWorkloadImagePatches(
	namespace string,
	resourceName string,
	containers []corev1.Container,
	initContainers []corev1.Container,
	mappings []RegistryMapping,
	policy disasterv1.ImageRewriteUnmatchedPolicy,
	unmatchedSet map[string]struct{},
) []disasterv1.JSONPatch {
	patches := make([]disasterv1.JSONPatch, 0)

	appendPatches := func(kind string, pathPrefix string, items []corev1.Container) {
		for idx := range items {
			original := strings.TrimSpace(items[idx].Image)
			if original == "" {
				continue
			}

			rewritten, matched := RewriteImage(original, mappings)
			if matched {
				if rewritten != original {
					patches = append(patches, disasterv1.JSONPatch{
						Operation: "replace",
						Path:      fmt.Sprintf("%s/%d/image", pathPrefix, idx),
						Value:     rewritten,
					})
				}
				continue
			}

			if policy == disasterv1.ImageRewriteUnmatchedPolicyFail {
				key := fmt.Sprintf("%s/%s %s[%d]=%s", namespace, resourceName, kind, idx, original)
				unmatchedSet[key] = struct{}{}
			}
		}
	}

	appendPatches("containers", "/spec/template/spec/containers", containers)
	appendPatches("initContainers", "/spec/template/spec/initContainers", initContainers)

	return patches
}

func normalizeRegistry(registry string) string {
	return strings.TrimSuffix(strings.TrimSpace(registry), "/")
}

func exactResourceRegex(name string) string {
	return "^" + regexp.QuoteMeta(name) + "$"
}
