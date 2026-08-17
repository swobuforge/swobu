package providertest

import (
	"bytes"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
)

func TestCacheSensitiveProjectionDropsOnlyExecutionFieldsAndPreservesArrays(t *testing.T) {
	document := func(raw string) carrier.Document { return carrier.Document{Raw: []byte(raw)} }
	first, err := CacheSensitiveProjection(document(`{"b":2,"messages":["a","b"],"prompt_cache_key":"one","stream":true,"session_id":"x","cache_control":{"type":"ephemeral"},"prompt_cache_options":{"mode":"explicit"},"prompt_cache_breakpoint":true}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := CacheSensitiveProjection(document(`{"messages":["a","b"],"b":2,"prompt_cache_key":"two","stream":false,"session_id":"y","cache_control":{"type":"ephemeral"},"prompt_cache_options":{"mode":"explicit"},"prompt_cache_breakpoint":true}`))
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("execution-only fields changed projection: %s != %s (%v)", first, second, err)
	}
	reordered, _ := CacheSensitiveProjection(document(`{"b":2,"messages":["b","a"],"cache_control":{"type":"ephemeral"},"prompt_cache_options":{"mode":"explicit"},"prompt_cache_breakpoint":true}`))
	if bytes.Equal(first, reordered) {
		t.Fatal("array reordering was normalized away")
	}
	changedControls := []string{
		`{"b":2,"messages":["a","b"],"cache_control":{"type":"persistent"},"prompt_cache_options":{"mode":"explicit"},"prompt_cache_breakpoint":true}`,
		`{"b":2,"messages":["a","b"],"cache_control":{"type":"ephemeral"},"prompt_cache_options":{"mode":"automatic"},"prompt_cache_breakpoint":true}`,
		`{"b":2,"messages":["a","b"],"cache_control":{"type":"ephemeral"},"prompt_cache_options":{"mode":"explicit"},"prompt_cache_breakpoint":false}`,
	}
	for _, raw := range changedControls {
		changedControl, err := CacheSensitiveProjection(document(raw))
		if err != nil || bytes.Equal(first, changedControl) {
			t.Fatalf("cache-sensitive request control was normalized away: %s (%v)", raw, err)
		}
	}
}
