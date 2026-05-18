// Effect types for outbound async orchestration.
package effect

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

const modelCatalogProbeLoadTimeout = 8 * time.Second

type daemonRawTrafficRow struct {
	RequestID      string                     `json:"request_id"`
	ClientHandler  string                     `json:"client_handler,omitempty"`
	ClientProtocol string                     `json:"client_protocol,omitempty"`
	IngressFamily  string                     `json:"ingress_family,omitempty"`
	NormalizedOp   string                     `json:"normalized_op,omitempty"`
	Route          string                     `json:"route"`
	Result         string                     `json:"result"`
	StatusCode     int                        `json:"status_code"`
	ObservedAt     string                     `json:"observed_at,omitempty"`
	Timing         *daemonRawTimingFields     `json:"timing,omitempty"`
	TokenUsage     *daemonRawTokenUsageFields `json:"token_usage,omitempty"`
}

type daemonRawTimingFields struct {
	TTFBMillis *int `json:"ttfb_millis,omitempty"`
	DurMillis  *int `json:"dur_millis,omitempty"`
}

type daemonRawTokenUsageFields struct {
	InputTokens      *int `json:"input_tokens,omitempty"`
	OutputTokens     *int `json:"output_tokens,omitempty"`
	CacheReadTokens  *int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
}

type statusProjectionDoc struct {
	Scope         statusProjectionScope `json:"scope"`
	RecentTraffic []daemonRawTrafficRow `json:"recent_traffic"`
}

type statusProjectionScope struct {
	Kind     string `json:"kind"`
	Endpoint string `json:"endpoint,omitempty"`
}

// ScheduleDaemonRefreshEffect emits one daemon refresh tick after the delay.
// Zero delay is used for immediate mount-time kickoff.
type ScheduleDaemonRefreshEffect struct {
	Delay time.Duration
}

func (eff ScheduleDaemonRefreshEffect) Execute(ctx context.Context) []update.Action {
	if eff.Delay > 0 {
		timer := time.NewTimer(eff.Delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
	}
	return []update.Action{DaemonRefreshTick{}}
}

// DaemonRefreshTick asks the reducer to run one full daemon control-plane sync
// and schedule the next periodic refresh.
type DaemonRefreshTick struct{}

// RefreshDaemonStatusEffect queries daemon health and reports status back to the UI.
type RefreshDaemonStatusEffect struct{}

func (RefreshDaemonStatusEffect) Execute(ctx context.Context) []update.Action {
	type daemonStatus struct {
		State                string `json:"state"`
		EndpointCount        int    `json:"endpoint_count"`
		ControlPlaneProtocol *int   `json:"control_plane_protocol,omitempty"`
		SwobuVersion         string `json:"swobu_version"`
	}
	status, err := loadJSON[daemonStatus](ctx, platformconfig.DefaultDaemonURL()+"/_swobu/status")
	if err != nil {
		return []update.Action{DaemonStatusLoadFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	if strings.TrimSpace(status.SwobuVersion) == "" { // swobu:io-string source=boundary
		return []update.Action{ControlPlaneIncompatibleDetected{
			ExpectedProtocol:  controlplane.Protocol,
			TUIVersion:        controlplane.SwobuVersion(),
			DaemonVersion:     "missing required swobu_version",
			HasDaemonProtocol: false,
			Reason:            "status payload is missing required swobu_version",
		}}
	}
	if status.ControlPlaneProtocol == nil {
		return []update.Action{ControlPlaneIncompatibleDetected{
			ExpectedProtocol:  controlplane.Protocol,
			TUIVersion:        controlplane.SwobuVersion(),
			DaemonVersion:     strings.TrimSpace(status.SwobuVersion), // swobu:io-string source=boundary
			HasDaemonProtocol: false,
			Reason:            "status payload is missing required control_plane_protocol",
		}}
	}
	if *status.ControlPlaneProtocol != controlplane.Protocol {
		return []update.Action{ControlPlaneIncompatibleDetected{
			ExpectedProtocol:  controlplane.Protocol,
			DaemonProtocol:    *status.ControlPlaneProtocol,
			TUIVersion:        controlplane.SwobuVersion(),
			DaemonVersion:     strings.TrimSpace(status.SwobuVersion), // swobu:io-string source=boundary
			HasDaemonProtocol: true,
			Reason:            "control-plane protocol mismatch",
		}}
	}
	return []update.Action{ReplaceDaemonStatus{
		State:         status.State,
		EndpointCount: status.EndpointCount,
	}}
}

// RefreshEndpointsEffect queries endpoint list and reports it back to the UI.
type RefreshEndpointsEffect struct{}

func (RefreshEndpointsEffect) Execute(ctx context.Context) []update.Action {
	c := operatorClient()
	endpoints, err := c.List(ctx)
	if err != nil {
		return []update.Action{EndpointsLoadFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	snapshots := make([]stateModel.EndpointSnapshot, 0, len(endpoints))
	for _, ep := range endpoints {
		snapshots = append(snapshots, endpointToSnapshot(ep))
	}
	return []update.Action{ReplaceEndpoints{Snapshots: snapshots}}
}

// RefreshStatusProjectionEffect queries recent traffic and reports it back to the UI.
type RefreshStatusProjectionEffect struct {
	EndpointName string
}

func (eff RefreshStatusProjectionEffect) Execute(ctx context.Context) []update.Action {
	requestedScope := statusProjectionScope{Kind: "all"}
	if endpoint := strings.TrimSpace(eff.EndpointName); endpoint != "" { // swobu:io-string source=boundary
		requestedScope = statusProjectionScope{
			Kind:     "endpoint",
			Endpoint: endpoint,
		}
	}
	query := url.Values{}
	if requestedScope.Kind == "endpoint" {
		query.Set("scope", "endpoint:"+requestedScope.Endpoint)
	} else {
		query.Set("scope", "all")
	}
	result, err := loadJSONValidated[statusProjectionDoc](ctx, platformconfig.DefaultDaemonURL()+"/_swobu/status-projection?"+query.Encode(), func(d statusProjectionDoc) error {
		return validateStatusProjectionDoc(d, requestedScope)
	})
	if err != nil {
		return []update.Action{TrafficLoadFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	rows := make([]stateModel.TrafficRow, 0, len(result.RecentTraffic))
	for _, r := range result.RecentTraffic {
		var ttfbMillis *int
		var durMillis *int
		if r.Timing != nil {
			ttfbMillis = r.Timing.TTFBMillis
			durMillis = r.Timing.DurMillis
		}
		var inputTokens *int
		var outputTokens *int
		var cacheReadTokens *int
		var cacheWriteTokens *int
		if r.TokenUsage != nil {
			inputTokens = r.TokenUsage.InputTokens
			outputTokens = r.TokenUsage.OutputTokens
			cacheReadTokens = r.TokenUsage.CacheReadTokens
			cacheWriteTokens = r.TokenUsage.CacheWriteTokens
		}
		rows = append(rows, stateModel.TrafficRow{
			RequestID:        r.RequestID,
			OperationFamily:  trafficOperationFamily(r.IngressFamily, r.Result, r.StatusCode),
			Target:           r.Route,
			Result:           r.Result,
			StatusCode:       r.StatusCode,
			ObservedAt:       r.ObservedAt,
			TTFBMillis:       ttfbMillis,
			DurMillis:        durMillis,
			InputTokens:      inputTokens,
			OutputTokens:     outputTokens,
			CacheReadTokens:  cacheReadTokens,
			CacheWriteTokens: cacheWriteTokens,
		})
	}
	return []update.Action{ReplaceStatusProjection{Rows: rows}}
}

func validateStatusProjectionDoc(d statusProjectionDoc, requestedScope statusProjectionScope) error {
	if strings.TrimSpace(d.Scope.Kind) == "" { // swobu:io-string source=boundary
		return fmt.Errorf("status projection scope is required")
	}
	if d.Scope.Kind != requestedScope.Kind {
		return fmt.Errorf("status projection scope kind mismatch: got %q want %q", d.Scope.Kind, requestedScope.Kind)
	}
	if d.Scope.Kind == "endpoint" && strings.TrimSpace(d.Scope.Endpoint) != requestedScope.Endpoint { // swobu:io-string source=boundary
		return fmt.Errorf("status projection scope endpoint mismatch: got %q want %q", d.Scope.Endpoint, requestedScope.Endpoint)
	}
	for i := range d.RecentTraffic {
		row := d.RecentTraffic[i]
		if strings.TrimSpace(row.RequestID) == "" { // swobu:io-string source=boundary
			return fmt.Errorf("status projection row %d missing request_id", i)
		}
		if strings.TrimSpace(row.Route) == "" { // swobu:io-string source=boundary
			return fmt.Errorf("status projection row %d missing route", i)
		}
		if strings.TrimSpace(row.Result) == "" { // swobu:io-string source=boundary
			return fmt.Errorf("status projection row %d missing result", i)
		}
		if strings.TrimSpace(row.ObservedAt) == "" { // swobu:io-string source=boundary
			return fmt.Errorf("status projection row %d missing observed_at", i)
		}
	}
	return nil
}

// --- Effect result action types ---

// DaemonStatusLoadFailed reports that daemon status could not be loaded.
type DaemonStatusLoadFailed struct{ Message string }

// ReplaceDaemonStatus carries the new daemon status.
type ReplaceDaemonStatus struct {
	State         string
	EndpointCount int
}

// EndpointsLoadFailed reports that endpoint list could not be loaded.
type EndpointsLoadFailed struct{ Message string }

// ReplaceEndpoints carries the new endpoint list.
type ReplaceEndpoints struct{ Snapshots []stateModel.EndpointSnapshot }

// TrafficLoadFailed reports that traffic projection could not be loaded.
type TrafficLoadFailed struct{ Message string }

// ReplaceStatusProjection carries the new traffic projection.
type ReplaceStatusProjection struct{ Rows []stateModel.TrafficRow }

// LoadRoutingModelCatalogEffect queries provider-backed model catalog for
// routing model selection across create/add scopes.
type LoadRoutingModelCatalogEffect struct {
	Scope         string
	ProviderSpec  string
	BaseURL       string
	CredentialRef string
}

func (eff LoadRoutingModelCatalogEffect) Execute(ctx context.Context) []update.Action {
	query := url.Values{}
	query.Set("provider_spec", strings.TrimSpace(eff.ProviderSpec)) // swobu:io-string source=boundary
	if baseURL := strings.TrimSpace(eff.BaseURL); baseURL != "" {   // swobu:io-string source=boundary
		query.Set("base_url", baseURL)
	}
	if credentialRef := strings.TrimSpace(eff.CredentialRef); credentialRef != "" { // swobu:io-string source=boundary
		query.Set("credential_ref", credentialRef)
	}
	type probeResult struct {
		ModelIDs []string `json:"model_ids,omitempty"`
		Error    string   `json:"error,omitempty"`
	}
	result, err := loadJSONWithTimeout[probeResult](ctx, platformconfig.DefaultDaemonURL()+"/_swobu/model-catalog/probe?"+query.Encode(), modelCatalogProbeLoadTimeout)
	if err != nil {
		normalized := normalizeModelCatalogProbeLoadError(err)
		return []update.Action{RoutingModelCatalogLoaded{
			Scope:         strings.TrimSpace(eff.Scope),         // swobu:io-string source=boundary
			ProviderSpec:  strings.TrimSpace(eff.ProviderSpec),  // swobu:io-string source=boundary
			BaseURL:       strings.TrimSpace(eff.BaseURL),       // swobu:io-string source=boundary
			CredentialRef: strings.TrimSpace(eff.CredentialRef), // swobu:io-string source=boundary
			Error:         normalized,
		}}
	}
	return []update.Action{RoutingModelCatalogLoaded{
		Scope:         strings.TrimSpace(eff.Scope),         // swobu:io-string source=boundary
		ProviderSpec:  strings.TrimSpace(eff.ProviderSpec),  // swobu:io-string source=boundary
		BaseURL:       strings.TrimSpace(eff.BaseURL),       // swobu:io-string source=boundary
		CredentialRef: strings.TrimSpace(eff.CredentialRef), // swobu:io-string source=boundary
		ModelIDs:      append([]string(nil), result.ModelIDs...),
		Error:         strings.TrimSpace(result.Error), // swobu:io-string source=boundary
	}}
}

// RoutingModelCatalogLoaded carries routing model catalog choices for one scope.
type RoutingModelCatalogLoaded struct {
	Scope         string
	ProviderSpec  string
	BaseURL       string
	CredentialRef string
	ModelIDs      []string
	Error         string
}

func normalizeOperatorSurfaceError(err error) string {
	message := strings.TrimSpace(err.Error())                                    // swobu:io-string source=boundary
	message = strings.TrimSpace(strings.TrimPrefix(message, "operator client:")) // swobu:io-string source=boundary
	if message == "" {
		return daemonUnavailableHint()
	}
	lower := strings.ToLower(message) // swobu:io-string source=boundary
	if strings.Contains(lower, "is unavailable") ||
		strings.Contains(lower, "request failed") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "deadline exceeded") ||
		strings.Contains(lower, "no such host") {
		return daemonUnavailableHint()
	}
	return message
}

func daemonUnavailableHint() string {
	return "unavailable at " + platformconfig.DefaultDaemonURL()
}

func normalizeModelCatalogProbeLoadError(err error) string {
	normalized := normalizeOperatorSurfaceError(err)
	if strings.Contains(strings.ToLower(normalized), "request timed out") { // swobu:io-string source=boundary
		return "model probe timed out at " + platformconfig.DefaultDaemonURL() + " (retry)"
	}
	return normalized
}
