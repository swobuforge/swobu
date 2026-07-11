package transform

type Stage string

const (
	StageClientWireIn       Stage = "client_request.wire_in"
	StageRequestDocumentOut Stage = "provider_request.wire_out"
	StageRequestDocumentIn  Stage = "provider_response.wire_in"
	StageSemanticEvents     Stage = "semantic.response_events"
)
