package carrier

// Stage identifies one carrier boundary segment where a value is observed or
// rewritten. Exchange role and path selection belong to ports and links, not
// to this metadata token.
type Stage string

const (
	// TODO due to duplex nature of every step, it's still ambiguous what is in and what is out. Clarify or rename if better names are found.
	StageClientRequestIn    Stage = "client_request.wire_in"
	StageProviderRequestOut Stage = "provider_request.wire_out"
	StageProviderIngressIn  Stage = "provider_response.wire_in"
	StageClientResponseOut  Stage = "client_response.wire_out"
)
