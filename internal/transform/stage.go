package transform

type Stage string

const (
	StageClientHTTPIn    Stage = "client_http_in"
	StageClientWireIn    Stage = "client_wire_in"
	StageSemanticRequest Stage = "semantic_request"

	StageProviderWireOut Stage = "provider_wire_out"
	StageProviderHTTPOut Stage = "provider_http_out"

	StageProviderHTTPIn Stage = "provider_http_in"
	StageProviderWireIn Stage = "provider_wire_in"
	StageSemanticEvents Stage = "semantic_events"

	StageClientWireOut Stage = "client_wire_out"
	StageClientHTTPOut Stage = "client_http_out"
)
