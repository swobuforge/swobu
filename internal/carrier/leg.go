package carrier

// Leg identifies one exchange boundary segment where a carrier is observed or
// transformed.
type Leg string

const (
	LegClientRequestIn    Leg = "client_request_in"
	LegProviderRequestOut Leg = "provider_request_out"
	LegProviderResponseIn Leg = "provider_response_in"
	LegClientResponseOut  Leg = "client_response_out"
)
