package protocols

import (
	"encoding/json"
	"math"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type TokenUsagePathSpec struct {
	InputPaths      [][]string
	OutputPaths     [][]string
	CacheReadPaths  [][]string
	CacheWritePaths [][]string
}

// ExtractTokenUsage reads provider token accounting from one raw payload using
// fallback path candidates for each normalized counter.
func ExtractTokenUsage(raw []byte, spec TokenUsagePathSpec) canonical.TokenUsage {
	if len(raw) == 0 {
		return canonical.NewUnknownTokenUsage()
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return canonical.NewUnknownTokenUsage()
	}
	input := findFirstInt(payload, spec.InputPaths)
	output := findFirstInt(payload, spec.OutputPaths)
	cacheRead := findFirstInt(payload, spec.CacheReadPaths)
	cacheWrite := findFirstInt(payload, spec.CacheWritePaths)
	usage, err := canonical.NewTokenUsageWithOptional(input, output, cacheRead, cacheWrite)
	if err != nil {
		return canonical.NewUnknownTokenUsage()
	}
	return usage
}

func findFirstInt(payload any, paths [][]string) *int {
	for _, path := range paths {
		if len(path) == 0 {
			continue
		}
		value, ok := lookupPath(payload, path)
		if !ok {
			continue
		}
		n, ok := asNonNegativeInt(value)
		if !ok {
			continue
		}
		return &n
	}
	return nil
}

func lookupPath(root any, path []string) (any, bool) {
	current := root
	for _, segment := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := obj[segment]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func asNonNegativeInt(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		if typed < 0 || typed > math.MaxInt || typed != math.Trunc(typed) {
			return 0, false
		}
		return int(typed), true
	case int:
		if typed < 0 {
			return 0, false
		}
		return typed, true
	case int64:
		if typed < 0 || typed > math.MaxInt {
			return 0, false
		}
		return int(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil || parsed < 0 || parsed > math.MaxInt {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}
