package operatorclient

// Runtime lane: daemon status projection and recent traffic transport DTOs.
type StatusProjection struct {
	State         string             `json:"state"`
	RecentTraffic []RecentTrafficRow `json:"recent_traffic"`
}

type RecentTrafficRow struct {
	RequestID string `json:"request_id"`
	Endpoint  string `json:"endpoint"`
	// ClientHandler is the normalized client identifier derived from ingress
	// metadata. It is the canonical label shown in Cockpit activity rows.
	ClientHandler     string                            `json:"client_handler,omitempty"`
	ClientProtocol    string                            `json:"client_protocol,omitempty"`
	ClientFamily      string                            `json:"client_family,omitempty"`
	NormalizedOp      string                            `json:"normalized_op,omitempty"`
	Route             string                            `json:"route"`
	Result            string                            `json:"result"`
	StatusCode        int                               `json:"status_code"`
	AttemptCount      int                               `json:"attempt_count"`
	FallbackRecovered bool                              `json:"fallback_recovered"`
	ObservedAt        string                            `json:"observed_at,omitempty"`
	Timing            *RecentTrafficTimingRecord        `json:"timing,omitempty"`
	TokenUsage        *RecentTrafficTokenUseRecord      `json:"token_usage,omitempty"`
	ReusablePrefix    RecentTrafficReusablePrefixRecord `json:"reusable_prefix"`

	ModelRequested        string                `json:"model_requested,omitempty"`
	ModelResolved         string                `json:"model_resolved,omitempty"`
	ModelResolutionMode   string                `json:"model_resolution_mode,omitempty"`
	WorkspaceRouteModelID string                `json:"workspace_route_model,omitempty"`
	ProviderSpec          string                `json:"provider_spec,omitempty"`
	ProviderModel         string                `json:"provider_model,omitempty"`
	TargetProtocol        string                `json:"target_protocol,omitempty"`
	TargetVersion         uint64                `json:"target_version,omitempty"`
	ExchangeDiagnostics   []string              `json:"exchange_diagnostics,omitempty"`
	StageReports          []ExchangeStageReport `json:"exchange_stage_reports,omitempty"`
}

type RecentTrafficTimingRecord struct {
	TTFBMillis *int `json:"ttfb_millis,omitempty"`
	DurMillis  *int `json:"dur_millis,omitempty"`
}

type RecentTrafficTokenUseRecord struct {
	InputTokens      *int `json:"input_tokens,omitempty"`
	OutputTokens     *int `json:"output_tokens,omitempty"`
	CacheReadTokens  *int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
}

type RecentTrafficReusablePrefixRecord struct {
	State      string `json:"state"`
	ChangeKind string `json:"change_kind,omitempty"`
}

type ExchangeStageReport struct {
	Stage   string   `json:"stage"`
	Carrier string   `json:"carrier"`
	Applied []string `json:"applied,omitempty"`
	Mutated bool     `json:"mutated"`
}
