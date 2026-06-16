package restore

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

// DynamicImageRewriteSummary records runtime image rewrite compilation statistics.
type DynamicImageRewriteSummary struct {
	EnabledRuleCount      int
	GeneratedRuleCount    int
	MatchedImageCount     int
	UnmatchedImageCount   int
	SkippedForbiddenCount int
	ConflictCount         int
}

// DynamicImageRewriteCompiler scans live source resources and compiles runtime image rewrite rules.
type DynamicImageRewriteCompiler struct{}

type DynamicImageRewriteCompileOptions struct {
	BaselineSourceCluster string
	BaselineTargetCluster string
}

type DynamicImageRewriteCompileOption func(*DynamicImageRewriteCompileOptions)

// WithDynamicImageRewriteBaseline configures baseline clusters for forward/reverse image matching.
func WithDynamicImageRewriteBaseline(sourceCluster, targetCluster string) DynamicImageRewriteCompileOption {
	return func(o *DynamicImageRewriteCompileOptions) {
		o.BaselineSourceCluster = strings.TrimSpace(sourceCluster)
		o.BaselineTargetCluster = strings.TrimSpace(targetCluster)
	}
}

type imageRewriteActionRule struct {
	actionIndex     int
	actionID        string
	sourcePrefix    string
	targetPrefix    string
	unmatchedPolicy disasterv1.ImageRewriteUnmatchedPolicy
	directionPolicy disasterv1.RestoreModifierDirectionPolicy
}

type imageRewriteMatch struct {
	rule        imageRewriteActionRule
	sourceImage string
	targetImage string
	conflict    bool
	conflicts   []imageRewriteResolvedCandidate
}

type imageRewriteResolvedCandidate struct {
	rule        imageRewriteActionRule
	sourceImage string
	targetImage string
}

type imageRewriteObject struct {
	groupResource string
	namespace     string
	name          string
	images        []imageFieldLocation
}

type imageFieldLocation struct {
	path  string
	image string
}

type imageRewriteRuntimeRule struct {
	conditions  disasterv1.Conditions
	path        string
	ruleID      string
	sourceImage string
	targetImage string
	direction   disasterv1.RestoreModifierDirectionPolicy
}

// HasDynamicImageRewriteActions reports whether policy has enabled rewriteImage actions for target.
func HasDynamicImageRewriteActions(policy *disasterv1.RestorePolicy, target disasterv1.RestoreModifierApplyTarget) bool {
	for _, action := range effectiveBulkModifierActions(policy) {
		if action.Action == disasterv1.BulkModifierActionRewriteImage && bulkActionAppliesToTarget(action, target) {
			return true
		}
	}
	return false
}

// CompileDynamicImageRewriteRules scans source resources and returns runtime modifier rules.
func (c *DynamicImageRewriteCompiler) CompileDynamicImageRewriteRules(
	ctx context.Context,
	instance *disasterv1.DisasterInstance,
	sourceClient client.Client,
	applyTarget disasterv1.RestoreModifierApplyTarget,
	opts ...DynamicImageRewriteCompileOption,
) ([]disasterv1.RestoreModifierRule, DynamicImageRewriteSummary, error) {
	summary := DynamicImageRewriteSummary{}
	if instance == nil || instance.Spec.RestorePolicy == nil || sourceClient == nil {
		return nil, summary, nil
	}
	options := applyDynamicImageRewriteCompileOptions(opts...)

	actions, err := collectImageRewriteActions(instance.Spec.RestorePolicy, applyTarget)
	if err != nil {
		return nil, summary, err
	}
	if len(actions) == 0 {
		return nil, summary, nil
	}
	flow, err := resolveDynamicImageRewriteFlow(instance, options)
	if err != nil {
		return nil, summary, err
	}
	actions = filterImageRewriteActionsForFlow(actions, flow)
	if len(actions) == 0 {
		return nil, summary, nil
	}
	summary.EnabledRuleCount = len(actions)

	objects, err := collectImageRewriteObjects(ctx, sourceClient, instance)
	if err != nil {
		return nil, summary, err
	}

	unmatched := make([]string, 0)
	seen := make(map[string]imageRewriteRuntimeRule)
	order := make([]string, 0)
	imageIndex := 0

	for _, obj := range objects {
		for _, loc := range obj.images {
			image := strings.TrimSpace(loc.image)
			if image == "" {
				continue
			}
			if isForbiddenModifierPath(loc.path) {
				summary.SkippedForbiddenCount++
				continue
			}

			match := matchImageRewrite(image, actions, flow)
			if match.conflict {
				summary.ConflictCount++
				return nil, summary, imageRewriteConflictError(obj, loc.path, image, match.conflicts)
			}
			if match.rule.sourcePrefix == "" {
				summary.UnmatchedImageCount++
				if anyImageRewriteActionFailsUnmatched(actions) {
					unmatched = append(unmatched, formatImageRewritePath(obj, loc.path, image))
				}
				continue
			}

			summary.MatchedImageCount++
			if pairValueForFlow(match.sourceImage, match.targetImage, flow) == image {
				continue
			}

			rule := imageRewriteRuntimeRule{
				conditions: disasterv1.Conditions{
					GroupResource:     obj.groupResource,
					ResourceNameRegex: exactResourceRegex(nameForRegex(obj.name)),
					Namespaces:        []string{obj.namespace},
				},
				path:        loc.path,
				ruleID:      fmt.Sprintf("runtime-image-rewrite-%04d-%04d", match.rule.actionIndex, imageIndex),
				sourceImage: match.sourceImage,
				targetImage: match.targetImage,
				direction:   normalizeDirectionPolicy(match.rule.directionPolicy),
			}
			key := makeConflictKey(rule.conditions, disasterv1.JSONPatch{
				Operation: "add",
				Path:      rule.path,
				Value:     rule.targetImage,
			})
			if existing, ok := seen[key]; ok {
				if existing.targetImage != rule.targetImage {
					summary.ConflictCount++
					return nil, summary, fmt.Errorf(
						"%s: image rewrite conflict resource=%s path=%s existingRule=%s currentRule=%s existingTarget=%s currentTarget=%s",
						ModifierErrorRuleConflict,
						formatImageRewriteObject(obj),
						loc.path,
						existing.ruleID,
						rule.ruleID,
						existing.targetImage,
						rule.targetImage,
					)
				}
				continue
			}
			seen[key] = rule
			order = append(order, key)
			imageIndex++
		}
	}

	if len(unmatched) > 0 {
		sort.Strings(unmatched)
		return nil, summary, imageRewriteUnmatchedError(unmatched)
	}

	out := make([]disasterv1.RestoreModifierRule, 0, len(order))
	for _, key := range order {
		rule := seen[key]
		out = append(out, disasterv1.RestoreModifierRule{
			ID:              rule.ruleID,
			Mode:            disasterv1.RestoreModifierModeReversible,
			ApplyTo:         []disasterv1.RestoreModifierApplyTarget{applyTarget},
			Priority:        -100,
			Conditions:      rule.conditions,
			DirectionPolicy: rule.direction,
			OnConflict:      disasterv1.RestoreModifierConflictPolicyFail,
			Pair: &disasterv1.RestoreModifierPair{
				Path:        rule.path,
				SourceValue: rule.sourceImage,
				TargetValue: rule.targetImage,
			},
		})
	}
	summary.GeneratedRuleCount = len(out)
	return out, summary, nil
}

func collectImageRewriteActions(
	policy *disasterv1.RestorePolicy,
	applyTarget disasterv1.RestoreModifierApplyTarget,
) ([]imageRewriteActionRule, error) {
	actions := effectiveBulkModifierActions(policy)
	if len(actions) == 0 {
		return nil, nil
	}
	out := make([]imageRewriteActionRule, 0, len(actions))
	for idx := range actions {
		action := actions[idx]
		if action.Action != disasterv1.BulkModifierActionRewriteImage {
			continue
		}
		if !bulkActionAppliesToTarget(action, applyTarget) {
			continue
		}
		cfg := action.ImageRewrite
		if cfg == nil {
			return nil, fmt.Errorf("%s: rewriteImage action %s missing imageRewrite", ModifierErrorRuleRejected, strings.TrimSpace(action.ID))
		}
		sourcePrefix := normalizeImagePrefix(cfg.SourcePrefix)
		targetPrefix := normalizeImagePrefix(cfg.TargetPrefix)
		if sourcePrefix == "" || targetPrefix == "" {
			return nil, fmt.Errorf("%s: rewriteImage action %s requires non-empty sourcePrefix/targetPrefix", ModifierErrorRuleRejected, strings.TrimSpace(action.ID))
		}
		if cfg.DigestPolicy != "" && cfg.DigestPolicy != disasterv1.ImageRewriteDigestPolicyPreserve {
			return nil, fmt.Errorf("%s: rewriteImage action %s unsupported digestPolicy=%s", ModifierErrorRuleRejected, strings.TrimSpace(action.ID), cfg.DigestPolicy)
		}
		out = append(out, imageRewriteActionRule{
			actionIndex:     idx,
			actionID:        strings.TrimSpace(action.ID),
			sourcePrefix:    sourcePrefix,
			targetPrefix:    targetPrefix,
			unmatchedPolicy: normalizeImageRewriteUnmatchedPolicy(cfg.UnmatchedPolicy),
			directionPolicy: action.DirectionPolicy,
		})
	}
	return out, nil
}

func applyDynamicImageRewriteCompileOptions(opts ...DynamicImageRewriteCompileOption) DynamicImageRewriteCompileOptions {
	out := DynamicImageRewriteCompileOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}

func resolveDynamicImageRewriteFlow(
	instance *disasterv1.DisasterInstance,
	options DynamicImageRewriteCompileOptions,
) (modifierFlow, error) {
	if instance == nil {
		return modifierFlowForward, nil
	}
	flow, _, err := resolveModifierFlow(
		options.BaselineSourceCluster,
		options.BaselineTargetCluster,
		instance.Status.PrimaryCluster,
		instance.Status.SecondaryCluster,
	)
	if err == nil {
		return flow, nil
	}
	if strings.TrimSpace(options.BaselineSourceCluster) == "" || strings.TrimSpace(options.BaselineTargetCluster) == "" {
		return modifierFlowForward, nil
	}
	return "", err
}

func bulkActionAppliesToTarget(action disasterv1.BulkModifierAction, target disasterv1.RestoreModifierApplyTarget) bool {
	if target == "" || len(action.ApplyTo) == 0 {
		return true
	}
	for _, t := range action.ApplyTo {
		if t == target {
			return true
		}
	}
	return false
}

func matchImageRewrite(image string, actions []imageRewriteActionRule, flow modifierFlow) imageRewriteMatch {
	current := strings.TrimSpace(image)
	if current == "" {
		return imageRewriteMatch{}
	}
	bestLen := -1
	candidates := make([]imageRewriteResolvedCandidate, 0, 1)
	for _, action := range actions {
		matchPrefix := action.sourcePrefix
		rewritePrefix := action.targetPrefix
		if flow == modifierFlowReverse {
			matchPrefix = action.targetPrefix
			rewritePrefix = action.sourcePrefix
		}
		if !imagePrefixMatches(current, matchPrefix) {
			continue
		}
		l := len(matchPrefix)
		rewritten := rewriteImageWithPrefix(current, matchPrefix, rewritePrefix)
		candidate := imageRewriteResolvedCandidate{rule: action}
		if flow == modifierFlowReverse {
			candidate.sourceImage = rewritten
			candidate.targetImage = current
		} else {
			candidate.sourceImage = current
			candidate.targetImage = rewritten
		}
		switch {
		case l > bestLen:
			bestLen = l
			candidates = candidates[:0]
			candidates = append(candidates, candidate)
		case l == bestLen:
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return imageRewriteMatch{}
	}

	targets := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		targets[pairValueForFlow(candidate.sourceImage, candidate.targetImage, flow)] = struct{}{}
	}
	if len(targets) > 1 {
		return imageRewriteMatch{conflict: true, conflicts: candidates}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].rule.actionIndex < candidates[j].rule.actionIndex
	})
	return imageRewriteMatch{
		rule:        candidates[0].rule,
		sourceImage: candidates[0].sourceImage,
		targetImage: candidates[0].targetImage,
	}
}

func imagePrefixMatches(image, prefix string) bool {
	image = strings.TrimSpace(image)
	prefix = normalizeImagePrefix(prefix)
	return image == prefix || strings.HasPrefix(image, prefix+"/")
}

func rewriteImageWithPrefix(image, sourcePrefix, targetPrefix string) string {
	image = strings.TrimSpace(image)
	sourcePrefix = normalizeImagePrefix(sourcePrefix)
	targetPrefix = normalizeImagePrefix(targetPrefix)
	if image == sourcePrefix {
		return targetPrefix
	}
	return targetPrefix + strings.TrimPrefix(image, sourcePrefix)
}

func pairValueForFlow(sourceImage, targetImage string, flow modifierFlow) string {
	if flow == modifierFlowReverse {
		return sourceImage
	}
	return targetImage
}

func anyImageRewriteActionFailsUnmatched(actions []imageRewriteActionRule) bool {
	for _, action := range actions {
		if action.unmatchedPolicy == disasterv1.ImageRewriteUnmatchedPolicyFail {
			return true
		}
	}
	return false
}

func filterImageRewriteActionsForFlow(actions []imageRewriteActionRule, flow modifierFlow) []imageRewriteActionRule {
	if len(actions) == 0 {
		return nil
	}
	out := make([]imageRewriteActionRule, 0, len(actions))
	for _, action := range actions {
		if directionPolicyAllows(action.directionPolicy, flow) {
			out = append(out, action)
		}
	}
	return out
}

func collectImageRewriteObjects(
	ctx context.Context,
	sourceClient client.Client,
	instance *disasterv1.DisasterInstance,
) ([]imageRewriteObject, error) {
	if instance == nil {
		return nil, nil
	}
	var selector client.ListOption
	if instance.Spec.LabelSelector != nil {
		sel, err := metav1.LabelSelectorAsSelector(instance.Spec.LabelSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector: %w", err)
		}
		selector = client.MatchingLabelsSelector{Selector: sel}
	}
	namespaces := append([]string{}, instance.Spec.Namespaces...)
	sort.Strings(namespaces)

	objects := make([]imageRewriteObject, 0)
	for _, namespace := range namespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			continue
		}
		opts := []client.ListOption{client.InNamespace(namespace)}
		if selector != nil {
			opts = append(opts, selector)
		}

		deployments := &appsv1.DeploymentList{}
		if err := sourceClient.List(ctx, deployments, opts...); err != nil {
			return nil, fmt.Errorf("list deployments in %s: %w", namespace, err)
		}
		for i := range deployments.Items {
			obj := deployments.Items[i]
			objects = append(objects, imageRewriteObject{
				groupResource: "deployments.apps",
				namespace:     obj.Namespace,
				name:          obj.Name,
				images:        collectPodSpecImages("/spec/template/spec", obj.Spec.Template.Spec),
			})
		}

		statefulSets := &appsv1.StatefulSetList{}
		if err := sourceClient.List(ctx, statefulSets, opts...); err != nil {
			return nil, fmt.Errorf("list statefulsets in %s: %w", namespace, err)
		}
		for i := range statefulSets.Items {
			obj := statefulSets.Items[i]
			objects = append(objects, imageRewriteObject{
				groupResource: "statefulsets.apps",
				namespace:     obj.Namespace,
				name:          obj.Name,
				images:        collectPodSpecImages("/spec/template/spec", obj.Spec.Template.Spec),
			})
		}

		daemonSets := &appsv1.DaemonSetList{}
		if err := sourceClient.List(ctx, daemonSets, opts...); err != nil {
			return nil, fmt.Errorf("list daemonsets in %s: %w", namespace, err)
		}
		for i := range daemonSets.Items {
			obj := daemonSets.Items[i]
			objects = append(objects, imageRewriteObject{
				groupResource: "daemonsets.apps",
				namespace:     obj.Namespace,
				name:          obj.Name,
				images:        collectPodSpecImages("/spec/template/spec", obj.Spec.Template.Spec),
			})
		}

		jobs := &batchv1.JobList{}
		if err := sourceClient.List(ctx, jobs, opts...); err != nil {
			return nil, fmt.Errorf("list jobs in %s: %w", namespace, err)
		}
		for i := range jobs.Items {
			obj := jobs.Items[i]
			objects = append(objects, imageRewriteObject{
				groupResource: "jobs.batch",
				namespace:     obj.Namespace,
				name:          obj.Name,
				images:        collectPodSpecImages("/spec/template/spec", obj.Spec.Template.Spec),
			})
		}

		cronJobs := &batchv1.CronJobList{}
		if err := sourceClient.List(ctx, cronJobs, opts...); err != nil {
			return nil, fmt.Errorf("list cronjobs in %s: %w", namespace, err)
		}
		for i := range cronJobs.Items {
			obj := cronJobs.Items[i]
			objects = append(objects, imageRewriteObject{
				groupResource: "cronjobs.batch",
				namespace:     obj.Namespace,
				name:          obj.Name,
				images:        collectPodSpecImages("/spec/jobTemplate/spec/template/spec", obj.Spec.JobTemplate.Spec.Template.Spec),
			})
		}

		pods := &corev1.PodList{}
		if err := sourceClient.List(ctx, pods, opts...); err != nil {
			return nil, fmt.Errorf("list pods in %s: %w", namespace, err)
		}
		for i := range pods.Items {
			obj := pods.Items[i]
			objects = append(objects, imageRewriteObject{
				groupResource: "pods",
				namespace:     obj.Namespace,
				name:          obj.Name,
				images:        collectPodSpecImages("/spec", obj.Spec),
			})
		}
	}

	sort.SliceStable(objects, func(i, j int) bool {
		if objects[i].groupResource != objects[j].groupResource {
			return objects[i].groupResource < objects[j].groupResource
		}
		if objects[i].namespace != objects[j].namespace {
			return objects[i].namespace < objects[j].namespace
		}
		return objects[i].name < objects[j].name
	})
	return objects, nil
}

func collectPodSpecImages(basePath string, spec corev1.PodSpec) []imageFieldLocation {
	locations := make([]imageFieldLocation, 0, len(spec.InitContainers)+len(spec.Containers)+len(spec.EphemeralContainers))
	for idx, c := range spec.Containers {
		locations = append(locations, imageFieldLocation{path: fmt.Sprintf("%s/containers/%d/image", basePath, idx), image: c.Image})
	}
	for idx, c := range spec.InitContainers {
		locations = append(locations, imageFieldLocation{path: fmt.Sprintf("%s/initContainers/%d/image", basePath, idx), image: c.Image})
	}
	for idx, c := range spec.EphemeralContainers {
		locations = append(locations, imageFieldLocation{path: fmt.Sprintf("%s/ephemeralContainers/%d/image", basePath, idx), image: c.Image})
	}
	return locations
}

func normalizeImagePrefix(prefix string) string {
	return strings.TrimSuffix(strings.TrimSpace(prefix), "/")
}

func normalizeImageRewriteUnmatchedPolicy(p disasterv1.ImageRewriteUnmatchedPolicy) disasterv1.ImageRewriteUnmatchedPolicy {
	switch p {
	case disasterv1.ImageRewriteUnmatchedPolicyFail:
		return disasterv1.ImageRewriteUnmatchedPolicyFail
	default:
		return disasterv1.ImageRewriteUnmatchedPolicyKeep
	}
}

func isForbiddenModifierPath(path string) bool {
	path = strings.TrimSpace(path)
	return path == "/status" ||
		strings.HasPrefix(path, "/status/") ||
		path == "/metadata/finalizers" ||
		strings.HasPrefix(path, "/metadata/finalizers/") ||
		path == "/metadata/ownerReferences" ||
		strings.HasPrefix(path, "/metadata/ownerReferences/")
}

func exactResourceRegex(name string) string {
	return "^" + regexp.QuoteMeta(name) + "$"
}

func nameForRegex(name string) string {
	return strings.TrimSpace(name)
}

func formatImageRewriteObject(obj imageRewriteObject) string {
	if strings.TrimSpace(obj.namespace) == "" {
		return fmt.Sprintf("%s/%s", obj.groupResource, obj.name)
	}
	return fmt.Sprintf("%s/%s/%s", obj.groupResource, obj.namespace, obj.name)
}

func formatImageRewritePath(obj imageRewriteObject, path, image string) string {
	return fmt.Sprintf("%s %s=%s", formatImageRewriteObject(obj), path, image)
}

func imageRewriteConflictError(obj imageRewriteObject, path, image string, candidates []imageRewriteResolvedCandidate) error {
	parts := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		action := candidate.rule
		id := action.actionID
		if id == "" {
			id = fmt.Sprintf("action[%d]", action.actionIndex)
		}
		parts = append(parts, fmt.Sprintf("%s:%s->%s", id, candidate.sourceImage, candidate.targetImage))
	}
	sort.Strings(parts)
	return fmt.Errorf(
		"%s: image rewrite conflict resource=%s path=%s image=%s actions=%s",
		ModifierErrorRuleConflict,
		formatImageRewriteObject(obj),
		path,
		image,
		strings.Join(parts, ","),
	)
}

func imageRewriteUnmatchedError(unmatched []string) error {
	const limit = 5
	if len(unmatched) > limit {
		return fmt.Errorf("%s: image rewrite unmatched (%d), first %d: %s", ModifierErrorRuleRejected, len(unmatched), limit, strings.Join(unmatched[:limit], "; "))
	}
	return fmt.Errorf("%s: image rewrite unmatched (%d): %s", ModifierErrorRuleRejected, len(unmatched), strings.Join(unmatched, "; "))
}
