package composition

// ProviderTransformFactRecord captures provider capability facts used by the
// exchange-owned transform chain.
type ProviderTransformFactRecord struct {
	CacheAffinityKey           string
	CacheAffinityRetention     string
	ReduceDuplicateUsageEvents bool
}
