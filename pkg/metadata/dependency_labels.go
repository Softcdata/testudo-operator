package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

type DependencyEdge struct {
	TargetToken  string
	RelationCode string
}

// BuildDependencyToken 由资源 UID 派生固定长度 token。
// 规则：sha256(uid) 前 16 位十六进制。
func BuildDependencyToken(uid string) string {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(uid))
	return hex.EncodeToString(sum[:])[:16]
}

// EnsureDependencyTokenLabel 确保 labels 中包含 dependency-token。
func EnsureDependencyTokenLabel(labels map[string]string, uid string) (map[string]string, string, bool) {
	token := BuildDependencyToken(uid)
	if labels == nil {
		labels = make(map[string]string)
	}
	if token == "" {
		return labels, "", false
	}

	if labels[LabelDependencyToken] == token {
		return labels, token, false
	}
	labels[LabelDependencyToken] = token
	return labels, token, true
}

func DependencyToLabelKey(token string) string {
	token = strings.TrimSpace(strings.ToLower(token))
	return LabelDependencyToPrefix + token
}

// RebuildDependencyToLabels 覆盖式重建 dependency-to-* 标签。
func RebuildDependencyToLabels(labels map[string]string, edges []DependencyEdge) (map[string]string, bool) {
	if labels == nil {
		labels = make(map[string]string)
	}

	next := make(map[string]string, len(edges))
	for _, edge := range edges {
		token := normalizeToken(edge.TargetToken)
		if token == "" {
			continue
		}
		next[DependencyToLabelKey(token)] = NormalizeRelationCode(edge.RelationCode)
	}

	changed := false
	for k, v := range labels {
		if !strings.HasPrefix(k, LabelDependencyToPrefix) {
			continue
		}
		if nv, ok := next[k]; !ok || nv != v {
			changed = true
			break
		}
	}
	if !changed {
		for k := range next {
			if ov, ok := labels[k]; !ok || ov != next[k] {
				changed = true
				break
			}
		}
	}

	if !changed {
		return labels, false
	}

	for k := range labels {
		if strings.HasPrefix(k, LabelDependencyToPrefix) {
			delete(labels, k)
		}
	}
	for k, v := range next {
		labels[k] = v
	}
	return labels, true
}

// NormalizeRelationCode 归一化 relation-code 为合法 label value。
func NormalizeRelationCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return "unknown"
	}

	var b strings.Builder
	b.Grow(len(code))
	for _, ch := range code {
		if isAlphaNum(ch) || ch == '-' || ch == '_' || ch == '.' {
			b.WriteRune(ch)
		} else {
			b.WriteByte('-')
		}
	}

	normalized := strings.TrimFunc(b.String(), func(r rune) bool {
		return !isAlphaNum(r)
	})
	if normalized == "" {
		return "unknown"
	}
	if len(normalized) > 63 {
		normalized = normalized[:63]
		normalized = strings.TrimFunc(normalized, func(r rune) bool {
			return !isAlphaNum(r)
		})
		if normalized == "" {
			return "unknown"
		}
	}
	return normalized
}

func normalizeToken(token string) string {
	token = strings.TrimSpace(strings.ToLower(token))
	if token == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(token))
	for _, ch := range token {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') {
			b.WriteRune(ch)
		}
	}
	normalized := b.String()
	if normalized == "" {
		return ""
	}
	if len(normalized) > 32 {
		return normalized[:32]
	}
	return normalized
}

func isAlphaNum(ch rune) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}
