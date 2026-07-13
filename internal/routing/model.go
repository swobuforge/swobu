package routing

import "time"

// WorkspaceRouting is the per-workspace routing configuration.
type WorkspaceRouting struct {
	WorkspaceSlug string
	ClientBaseURL string
	DefaultModel  string           // route model name
	Routes        map[string]Route // key = route model name
}

// Route is a client-visible model name plus routing behavior.
type Route struct {
	ModelName string
	Default   bool
	Enabled   bool
	Rules     RouteRules
	Targets   []Target
}

// RouteState is the runtime visibility state of a route.
type RouteState string

const (
	RouteIncomplete RouteState = "incomplete"
	RouteDisabled   RouteState = "disabled"
	RouteUsable     RouteState = "usable"
	RouteBlocked    RouteState = "blocked"
	RouteDegraded   RouteState = "degraded"
)

// RouteRules configure route-level execution behavior.
type RouteRules struct {
	Timeout          time.Duration
	Fallback         FallbackMode
	SkipUnfit        bool
	Cooldown         CooldownMode
	RetryableClasses map[FailureClass]bool
}

// FallbackMode controls whether routing retries across targets before streaming.
type FallbackMode string

const (
	FallbackBeforeStream FallbackMode = "before_stream"
	FallbackOff          FallbackMode = "off"
)

// CooldownMode controls cooldown behavior.
type CooldownMode string

const (
	CooldownAuto CooldownMode = "auto"
)

// DefaultRouteRules returns the V0 default route rules.
func DefaultRouteRules() RouteRules {
	return RouteRules{
		Timeout:   12 * time.Second,
		Fallback:  FallbackBeforeStream,
		SkipUnfit: true,
		Cooldown:  CooldownAuto,
		RetryableClasses: map[FailureClass]bool{
			FailureTimeout:     true,
			FailureRateLimited: true,
			FailureServerError: true,
			FailureOverloaded:  true,
			FailureNetwork:     true,
		},
	}
}

// Target is one backend/model Swobu may attempt for a route.
type Target struct {
	ID            string
	Provider      string
	CredentialRef string
	Model         string
	Protocol      ProtocolOverride
	Rank          int
	Weight        int
	Enabled       bool
}

// ProtocolOverride is an optional explicit protocol kind for a target.
// Empty (zero value) means Swobu infers protocol from the provider spec.
type ProtocolOverride string

// TargetState is the runtime eligibility state of one target for a specific request.
type TargetState string

const (
	TargetUsable          TargetState = "usable"
	TargetDisabled        TargetState = "disabled"
	TargetAuthMissing     TargetState = "auth_missing"
	TargetCoolingDown     TargetState = "cooling_down"
	TargetContextTooSmall TargetState = "context_too_small"
	TargetUnsupported     TargetState = "unsupported"
	TargetUnreachable     TargetState = "unreachable"
)

// Attempt is one execution try of one target for one request.
// Not persisted. Owned by the routing executor, not the UI.
type Attempt struct {
	WorkspaceSlug string
	RouteModel    string
	Target        Target
	Request       CanonicalRequest // defined in this package to avoid exchange dep
	Index         int
}

// CanonicalRequest is the minimal request contract consumed by routing.
// It intentionally duplicates a subset of internal/domain/canonical fields
// rather than importing that package to avoid import cycles.
// Future integration will use a type alias or adapter at the planner boundary.
// V0 fields are placeholders for items and tools to keep the model focused.
type CanonicalRequest struct {
	Model                string
	Items                []string // placeholder; full items in future
	Tools                []string // placeholder; full tools in future
	RequiresStreaming    bool
	RequiresTools        bool
	RequiresSchema       bool
	EstimatedInputTokens int
}

// RequestFacts captures request-time features used for fit filtering.
type RequestFacts struct {
	EstimatedInputTokens int
	RequiresStreaming    bool
	RequiresTools        bool
	RequiresSchema       bool
	Modalities           []Modality
}

// Modality is a content modality that a target may or may not support.
type Modality string

// FailureClass categorizes failures into retryable vs terminal buckets.
type FailureClass string

const (
	FailureTimeout     FailureClass = "timeout"
	FailureRateLimited FailureClass = "rate_limited"
	FailureServerError FailureClass = "server_error"
	FailureOverloaded  FailureClass = "overloaded"
	FailureNetwork     FailureClass = "network"
	FailureBadRequest  FailureClass = "bad_request"
	FailureAuth        FailureClass = "auth"
	FailureForbidden   FailureClass = "forbidden"
	FailureNotFound    FailureClass = "not_found"
	FailureUnsupported FailureClass = "unsupported"
	FailureUnknown     FailureClass = "unknown"
)

// IsRetryable reports whether a failure class is retryable before streaming.
// Unknown defaults to terminal.
func (f FailureClass) IsRetryable() bool {
	switch f {
	case FailureTimeout, FailureRateLimited, FailureServerError, FailureOverloaded, FailureNetwork:
		return true
	default:
		return false
	}
}

// TargetKey uniquely identifies a target in the cooldown store.
type TargetKey struct {
	Workspace string
	Route     string
	TargetID  string
}

// FilterReason records why a target was excluded from the attempt plan.
type FilterReason string

const (
	FilterDisabled             FilterReason = "disabled"
	FilterAuthMissing          FilterReason = "auth_missing"
	FilterContextTooSmall      FilterReason = "context_too_small"
	FilterToolsUnsupported     FilterReason = "tools_unsupported"
	FilterSchemaUnsupported    FilterReason = "schema_unsupported"
	FilterStreamingUnsupported FilterReason = "streaming_unsupported"
	FilterModalityUnsupported  FilterReason = "modality_unsupported"
	FilterCoolingDown          FilterReason = "cooling_down"
)
