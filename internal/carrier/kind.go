package carrier

// Kind identifies the active carrier representation at one stage.
type Kind string

const (
	KindHTTPEnvelope         Kind = "http_envelope"
	KindWireDocument         Kind = "wire_document"
	KindWireStream           Kind = "wire_stream"
	KindCanonicalRequest     Kind = "canonical_request_snapshot"
	KindCanonicalEventStream Kind = "canonical_event_stream"
)
