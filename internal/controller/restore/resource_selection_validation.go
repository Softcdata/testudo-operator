package restore

import (
	"fmt"
	"strings"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

const (
	ResourceSelectionErrorInvalid = "ResourceSelectionInvalid"
	resourceSelectionFieldPrefix  = "spec.restorePolicy.resourceSelection"
)

// ValidateRestoreResourceSelectionAtSubmission validates restorePolicy.resourceSelection
// in a fail-fast manner during create/update submission.
func ValidateRestoreResourceSelectionAtSubmission(p *disasterv1.RestoreResourceSelectionPolicy) error {
	if p == nil {
		return nil
	}

	if includeClusterResourcesIsTrue(p.IncludeClusterResources) {
		return validateResourceFilterSet(
			"includedResources",
			"excludedResources",
			p.IncludedResources,
			p.ExcludedResources,
		)
	}

	if hasScopedResourceFilters(p) {
		if err := validateResourceFilterSet(
			"includedNamespaceScopedResources",
			"excludedNamespaceScopedResources",
			p.IncludedNamespaceScopedResources,
			p.ExcludedNamespaceScopedResources,
		); err != nil {
			return err
		}
		return validateResourceFilterSet(
			"includedClusterScopedResources",
			"excludedClusterScopedResources",
			p.IncludedClusterScopedResources,
			p.ExcludedClusterScopedResources,
		)
	}

	return validateResourceFilterSet(
		"includedResources",
		"excludedResources",
		p.IncludedResources,
		p.ExcludedResources,
	)
}

func validateResourceFilterSet(includeField string, excludeField string, includeRaw []string, excludeRaw []string) error {
	include := normalizeResourceFilterValues(includeRaw)
	exclude := normalizeResourceFilterValues(excludeRaw)
	if len(include) == 0 || len(exclude) == 0 {
		return nil
	}

	includeSet := make(map[string]struct{}, len(include))
	for _, item := range include {
		includeSet[item] = struct{}{}
	}
	excludeSet := make(map[string]struct{}, len(exclude))
	for _, item := range exclude {
		excludeSet[item] = struct{}{}
	}

	includePath := resourceSelectionFieldPrefix + "." + includeField
	excludePath := resourceSelectionFieldPrefix + "." + excludeField

	if _, hasWildcard := includeSet["*"]; hasWildcard {
		return fmt.Errorf(
			"%s: %s contains '*' and cannot be combined with %s",
			ResourceSelectionErrorInvalid,
			includePath,
			excludePath,
		)
	}
	if _, hasWildcard := excludeSet["*"]; hasWildcard {
		return fmt.Errorf(
			"%s: %s contains '*' and cannot be combined with %s",
			ResourceSelectionErrorInvalid,
			excludePath,
			includePath,
		)
	}

	for _, item := range include {
		if _, conflict := excludeSet[item]; conflict {
			return fmt.Errorf(
				"%s: %s and %s conflict on resource %q",
				ResourceSelectionErrorInvalid,
				includePath,
				excludePath,
				item,
			)
		}
	}
	return nil
}

func normalizeResourceFilterValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		v := strings.TrimSpace(value)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
