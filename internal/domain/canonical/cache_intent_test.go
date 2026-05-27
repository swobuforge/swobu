package canonical

import "testing"

func TestCacheIntentFromAffinityKeyAndRetention_DefaultsWhenUnset(t *testing.T) {
	intent := CacheIntentFromAffinityKeyAndRetention("", CacheRetentionUnset)
	if !intent.IsZero() {
		t.Fatalf("intent is not zero")
	}
}

func TestCacheIntentFromAffinityKeyAndRetention_PreservesConfiguredFields(t *testing.T) {
	intent := CacheIntentFromAffinityKeyAndRetention("repo", CacheRetention24H)
	if intent.Key() != "repo" {
		t.Fatalf("key=%q want repo", intent.Key())
	}
	if intent.Retention() != CacheRetention24H {
		t.Fatalf("retention=%q want %q", intent.Retention(), CacheRetention24H)
	}
}
