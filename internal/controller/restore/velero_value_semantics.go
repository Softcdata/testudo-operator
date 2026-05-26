package restore

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

type veleroPatchValueKind string

const (
	veleroPatchValueKindString  veleroPatchValueKind = "string"
	veleroPatchValueKindNumber  veleroPatchValueKind = "number"
	veleroPatchValueKindBoolean veleroPatchValueKind = "boolean"
	veleroPatchValueKindObject  veleroPatchValueKind = "object"
	veleroPatchValueKindArray   veleroPatchValueKind = "array"
	veleroPatchValueKindNull    veleroPatchValueKind = "null"
)

func encodePairValueForVeleroPath(path string, rawValue string) string {
	if !pathUsesStringPreservingMetadataMap(path) {
		return rawValue
	}
	return strconv.Quote(rawValue)
}

func pathUsesStringPreservingMetadataMap(path string) bool {
	tokens, err := decodeJSONPointerPath(path)
	if err != nil {
		return false
	}
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i] != "metadata" {
			continue
		}
		switch tokens[i+1] {
		case "annotations", "labels":
			return true
		}
	}
	return false
}

func decodeJSONPointerPath(path string) ([]string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("patch path is empty")
	}
	if path == "/" {
		return nil, fmt.Errorf("path / is not allowed")
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("path must start with /")
	}

	rawTokens := strings.Split(path[1:], "/")
	tokens := make([]string, 0, len(rawTokens))
	for _, raw := range rawTokens {
		token, err := decodeJSONPointerToken(raw)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

func resolveJSONPointerValue(document map[string]any, path string, operation string) (any, error) {
	tokens, err := decodeJSONPointerPath(path)
	if err != nil {
		return nil, err
	}

	var current any = document
	for i, token := range tokens {
		isLast := i == len(tokens)-1

		switch node := current.(type) {
		case map[string]any:
			next, ok := node[token]
			if !ok {
				if isLast && strings.EqualFold(strings.TrimSpace(operation), "add") {
					return nil, nil
				}
				return nil, fmt.Errorf("path segment %q not found", token)
			}
			current = next
		case []any:
			if token == "-" {
				if isLast && strings.EqualFold(strings.TrimSpace(operation), "add") {
					return nil, nil
				}
				return nil, fmt.Errorf("array segment '-' is only allowed for add at final token")
			}
			idx, convErr := strconv.Atoi(token)
			if convErr != nil {
				return nil, fmt.Errorf("array segment %q is not an integer index", token)
			}
			if idx < 0 || idx >= len(node) {
				return nil, fmt.Errorf("array index %d out of bounds", idx)
			}
			current = node[idx]
		default:
			return nil, fmt.Errorf("path segment %q is not traversable", token)
		}
	}

	return current, nil
}

func ensureJSONPointerLocatable(document map[string]any, path string, operation string) error {
	_, err := resolveJSONPointerValue(document, path, operation)
	return err
}

func validatePairValueCompatibility(
	path string,
	pair *disasterv1.RestoreModifierPair,
	document map[string]any,
	options ApplyInstanceRestorePolicyOptions,
) error {
	if pair == nil {
		return nil
	}

	currentValue, err := resolveJSONPointerValue(document, path, "add")
	if err != nil {
		return err
	}
	currentKind, known := classifyVeleroPatchValueKind(currentValue)
	if !known || currentKind == veleroPatchValueKindNull {
		return nil
	}

	checks := []struct {
		label string
		flow  modifierFlow
	}{
		{label: "pair.targetValue", flow: modifierFlowForward},
		{label: "pair.sourceValue", flow: modifierFlowReverse},
	}
	for _, check := range checks {
		rendered, err := resolvePairValue(pair, check.flow, options)
		if err != nil {
			return err
		}
		intendedKind, err := inferVeleroPatchValueKind(encodePairValueForVeleroPath(path, rendered))
		if err != nil {
			return fmt.Errorf("%s=%q is not a valid Velero patch literal: %v", check.label, rendered, err)
		}
		if intendedKind != currentKind {
			return fmt.Errorf("%s=%q would be applied as %s but live field type is %s", check.label, rendered, intendedKind, currentKind)
		}
	}

	return nil
}

func inferVeleroPatchValueKind(rawValue string) (veleroPatchValueKind, error) {
	if rawValue == "" {
		return veleroPatchValueKindString, nil
	}
	if strings.HasPrefix(rawValue, "\"") && strings.HasSuffix(rawValue, "\"") {
		return veleroPatchValueKindString, nil
	}
	if rawValue == "null" {
		return veleroPatchValueKindNull, nil
	}

	lower := strings.ToLower(rawValue)
	if lower == "true" || lower == "false" {
		return veleroPatchValueKindBoolean, nil
	}

	if strings.HasPrefix(rawValue, "{") || strings.HasPrefix(rawValue, "[") {
		var decoded any
		if err := json.Unmarshal([]byte(rawValue), &decoded); err != nil {
			return "", err
		}
		kind, known := classifyVeleroPatchValueKind(decoded)
		if !known {
			return "", fmt.Errorf("unsupported JSON literal type %T", decoded)
		}
		return kind, nil
	}

	if _, err := strconv.ParseFloat(rawValue, 64); err == nil {
		return veleroPatchValueKindNumber, nil
	}

	return veleroPatchValueKindString, nil
}

func classifyVeleroPatchValueKind(value any) (veleroPatchValueKind, bool) {
	switch value.(type) {
	case string:
		return veleroPatchValueKindString, true
	case bool:
		return veleroPatchValueKindBoolean, true
	case nil:
		return veleroPatchValueKindNull, true
	case float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return veleroPatchValueKindNumber, true
	case map[string]any:
		return veleroPatchValueKindObject, true
	case []any:
		return veleroPatchValueKindArray, true
	default:
		return "", false
	}
}
