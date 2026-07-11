package openaifamily

import (
	"github.com/swobuforge/swobu/internal/report"
	"github.com/swobuforge/swobu/internal/transform"
	"github.com/swobuforge/swobu/internal/transforms/composition"
)

func newTransformRegistry(facts ProfileFactRecord) transform.Registry {
	return composition.NewProviderTransformRegistry(composition.ProviderTransformFactRecord{
		CacheAffinityKey:           facts.CacheAffinityKey,
		CacheAffinityRetention:     facts.CacheAffinityRetention,
		ReduceDuplicateUsageEvents: facts.ReduceDuplicateUsageEvents,
	})
}

func transformFactNotices(facts ProfileFactRecord) []report.Notice {
	if !facts.CacheRetentionUnsupported {
		return nil
	}
	return []report.Notice{{
		Code:   "cache_retention_unsupported",
		Field:  "cache.retention",
		Reason: "ollama route does not expose prompt cache retention control",
	}}
}
