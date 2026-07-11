package observation

// ObservationRecord captures one runtime fact seen on an exchange path.
type ObservationRecord struct {
	RouteID    string
	ProviderID string
	ModelID    string
	Code       string
	Reason     string
	ObservedAt int64
	TTLSeconds int64
}

// ObservationQuerySpec scopes observation reads for one routing/model slice.
type ObservationQuerySpec struct {
	RouteID    string
	ProviderID string
	ModelID    string
	Code       string
}
