package metadata

import (
	"sort"
	"strings"
)

// BuildUpstreamLabelKeyFromTargetLabels returns selector key for querying upstream resources.
// It expects target labels to contain dependency-token.
func BuildUpstreamLabelKeyFromTargetLabels(labels map[string]string) (string, bool) {
	if len(labels) == 0 {
		return "", false
	}
	token := normalizeToken(labels[LabelDependencyToken])
	if token == "" {
		return "", false
	}
	return DependencyToLabelKey(token), true
}

// ParseDownstreamFromLabels parses dependency-to-* labels from a resource's own labels.
// Returned edges are sorted for deterministic behavior.
func ParseDownstreamFromLabels(labels map[string]string) []DependencyEdge {
	if len(labels) == 0 {
		return nil
	}

	edges := make([]DependencyEdge, 0)
	for k, v := range labels {
		if !strings.HasPrefix(k, LabelDependencyToPrefix) {
			continue
		}
		token := normalizeToken(strings.TrimPrefix(k, LabelDependencyToPrefix))
		if token == "" {
			continue
		}
		edges = append(edges, DependencyEdge{
			TargetToken:  token,
			RelationCode: NormalizeRelationCode(v),
		})
	}

	sort.Slice(edges, func(i, j int) bool {
		if edges[i].TargetToken == edges[j].TargetToken {
			return edges[i].RelationCode < edges[j].RelationCode
		}
		return edges[i].TargetToken < edges[j].TargetToken
	})
	return edges
}

// CanDeleteFromUpstreamCount derives can_delete from upstream count.
func CanDeleteFromUpstreamCount(upstreamCount int) bool {
	return upstreamCount == 0
}
