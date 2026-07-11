package transform

type Stage string

const (
	StageClientWireIn    Stage = "client_wire_in"
	StageProviderWireOut Stage = "provider_wire_out"
	StageProviderWireIn  Stage = "provider_wire_in"
	StageSemanticEvents  Stage = "semantic_events"
)
