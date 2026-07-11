package cacheaffinity

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
)

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
