package cacheaffinity

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/report"
)

type ApplyResult struct {
	Mutated bool
	Losses  []report.Loss
}

func Apply(doc carrier.WireDocument, key string, retention string) (carrier.WireDocument, ApplyResult, error) {
	if doc.IsEmpty() {
		return doc, ApplyResult{}, nil
	}
	out, mutated, err := mutateWireDocumentJSON(doc, func(payload map[string]any) (bool, error) {
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

func mutateWireDocumentJSON(doc carrier.WireDocument, mutate func(payload map[string]any) (bool, error)) (carrier.WireDocument, bool, error) {
	var payload map[string]any
	if len(doc.Raw) != 0 {
		payload = map[string]any{}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return carrier.WireDocument{}, false, err
		}
	} else {
		payload = map[string]any{}
	}
	changed, err := mutate(payload)
	if err != nil {
		return carrier.WireDocument{}, false, err
	}
	if !changed {
		return doc, false, nil
	}
	nextRaw, err := json.Marshal(payload)
	if err != nil {
		return carrier.WireDocument{}, false, err
	}
	doc.Raw = nextRaw
	return doc, true, nil
}

func TestApply_MutatesPromptCacheFields(t *testing.T) {
	in := carrier.WireDocument{Raw: []byte(`{"model":"m"}`)}
	out, result, err := Apply(in, "repo-alpha", "24h")
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !result.Mutated {
		t.Fatal("expected mutation")
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Raw, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got, _ := payload["prompt_cache_key"].(string); got != "repo-alpha" {
		t.Fatalf("prompt_cache_key=%q", got)
	}
	if got, _ := payload["prompt_cache_retention"].(string); got != "24h" {
		t.Fatalf("prompt_cache_retention=%q", got)
	}
}

func TestApply_IdempotentWhenFieldsAlreadySet(t *testing.T) {
	in := carrier.WireDocument{Raw: []byte(`{"model":"m","prompt_cache_key":"repo-alpha","prompt_cache_retention":"24h"}`)}
	out, result, err := Apply(in, "repo-alpha", "24h")
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.Mutated {
		t.Fatal("unexpected mutation")
	}
	if string(out.Raw) != string(in.Raw) {
		t.Fatalf("output changed unexpectedly: %s", string(out.Raw))
	}
}
