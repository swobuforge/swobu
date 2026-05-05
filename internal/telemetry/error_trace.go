package telemetry

// ErrorTrace is the bounded, content-free error signal emitted to OTLP trace
// exporters for proactive reliability triage.
type ErrorTrace struct {
	StatusCode    int
	ResultClass   string
	ProviderRoute string
	Operation     string
	DurationMS    *int
	DebugRawStack string
}
