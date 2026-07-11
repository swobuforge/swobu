package composition

// ProviderTransformFactRecord captures provider capability facts used by the
// exchange-owned transform chain.
type ProviderTransformFactRecord struct {
	CacheAffinityKey                string
	CacheAffinityRetention          string
	NormalizeToolDeclarations       bool
	StrictJSONSupportedRequestField map[string]struct{}
	ReduceDuplicateUsageEvents      bool
}
