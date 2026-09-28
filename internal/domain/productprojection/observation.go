package productprojection

// Observation is the final privacy-bounded product telemetry value. Its
// dimensions and timing are intentionally independent of execution identity,
// request content, provider error prose, and the canonical HTTP wire status.
type Observation struct {
	ClientFamily        string
	RequestedModel      string
	ResolvedModel       string
	ClientProtocol      string
	Operation           string
	Provider            string
	TargetProtocol      string
	Result              string
	StatusCode          int
	DeliveryKind        string
	ErrorCode           string
	AttemptCount        int
	DurationMS          *int
	TTFBMS              *int
	FallbackRecovered   bool
	ContinuityRecovered bool
	WireMutated         bool
	CrossProtocol       bool
}
