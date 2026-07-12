package carrier

// Stage identifies one carrier boundary segment where a value is observed or
// transformed. Exchange role and path selection belong to ports and links, not
// to this metadata token.
type Stage string

const (
	StageClientRequestIn    Stage = "client_request.wire_in"
	StageProviderRequestOut Stage = "provider_request.wire_out"
	StageProviderIngressIn  Stage = "provider_response.wire_in"
	StageClientResponseOut  Stage = "client_response.wire_out"
)
