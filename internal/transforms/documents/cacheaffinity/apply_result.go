package cacheaffinity

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/report"
)

type ApplyResult struct {
	Mutated bool
	Losses  []report.Loss
}

func Apply(doc carrier.WireDocument, key string, retention string) (carrier.WireDocument, ApplyResult, error) {
	if len(doc.Raw) == 0 {
		return doc, ApplyResult{}, nil
	}
	out, mutated, err := carrier.MutateWireDocumentJSON(doc, func(payload map[string]any) (bool, error) {
		changed := false
		if key != "" {
			if got, _ := payload["prompt_cache_key"].(string); got != key {
				payload["prompt_cache_key"] = key
				changed = true
			}
		}
		if retention != "" {
			if got, _ := payload["prompt_cache_retention"].(string); got != retention {
				payload["prompt_cache_retention"] = retention
				changed = true
			}
		}
		return changed, nil
	})
	if err != nil {
		return carrier.WireDocument{}, ApplyResult{}, canonical.InternalError("provider request body is invalid JSON for cache affinity transform")
	}
	return out, ApplyResult{Mutated: mutated}, nil
}
