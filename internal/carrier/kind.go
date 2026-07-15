package carrier

// Kind identifies the active carrier representation at one stage.
type Kind string

const (
	KindHTTPEnvelope         Kind = "http_envelope"
	KindCarrierDocument      Kind = "wire_document"
	KindCarrierStream        Kind = "wire_stream"
	KindCanonicalRequest     Kind = "canonical_request_snapshot"
	KindCanonicalEventStream Kind = "canonical_event_stream"
)
