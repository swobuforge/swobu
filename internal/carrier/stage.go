package carrier

// Stage identifies one exchange boundary segment where a carrier is observed or
// transformed.
type Stage string

const (
	StageClientRequestIn    Stage = "client_request.wire_in"
	StageProviderRequestOut Stage = "provider_request.wire_out"
	StageProviderIngressIn  Stage = "provider_response.wire_in"
	StageClientResponseOut  Stage = "client_response.wire_out"
)
