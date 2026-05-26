package metadata

type CleanupDescriptor struct {
	OwnerUID     string
	RelationCode string
	Strategy     string
}

func EnsureCleanupLabels(labels map[string]string, descriptor CleanupDescriptor) (map[string]string, bool) {
	if labels == nil {
		labels = make(map[string]string)
	}

	ownerToken := BuildDependencyToken(descriptor.OwnerUID)
	relationCode := NormalizeRelationCode(descriptor.RelationCode)
	strategy := NormalizeRelationCode(descriptor.Strategy)
	if strategy == "unknown" {
		strategy = CleanupStrategyDelete
	}

	next := map[string]string{
		LabelCleanupManagedBy: LabelCleanupManagedByValueOperator,
	}
	if ownerToken != "" {
		next[LabelCleanupOwnerToken] = ownerToken
	}
	if relationCode != "" {
		next[LabelCleanupRelation] = relationCode
	}
	if strategy != "" {
		next[LabelCleanupStrategy] = strategy
	}

	changed := false
	for key, value := range next {
		if labels[key] != value {
			changed = true
			break
		}
	}
	if !changed {
		for _, key := range []string{LabelCleanupOwnerToken, LabelCleanupRelation, LabelCleanupStrategy, LabelCleanupManagedBy} {
			if _, ok := next[key]; !ok && labels[key] != "" {
				changed = true
				break
			}
		}
	}

	if !changed {
		return labels, false
	}

	for _, key := range []string{LabelCleanupOwnerToken, LabelCleanupRelation, LabelCleanupStrategy, LabelCleanupManagedBy} {
		delete(labels, key)
	}
	for key, value := range next {
		labels[key] = value
	}

	return labels, true
}
