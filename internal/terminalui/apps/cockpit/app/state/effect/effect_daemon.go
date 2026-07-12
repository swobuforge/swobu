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
	RequestID           string                     `json:"request_id"`
	ClientHandler       string                     `json:"client_handler,omitempty"`
	ClientProtocol      string                     `json:"client_protocol,omitempty"`
	ClientFamily        string                     `json:"client_family,omitempty"`
	NormalizedOp        string                     `json:"normalized_op,omitempty"`
	Route               string                     `json:"route"`
	Result              string                     `json:"result"`
	StatusCode          int                        `json:"status_code"`
	ObservedAt          string                     `json:"observed_at,omitempty"`
	Timing              *daemonRawTimingFields     `json:"timing,omitempty"`
	TokenUsage          *daemonRawTokenUsageFields `json:"token_usage,omitempty"`
	Mutations           []stateModel.Mutation      `json:"wire_patch_mutations,omitempty"`
	ExchangeDiagnostics []string                   `json:"exchange_diagnostics,omitempty"`
	StageReports        []stateModel.StageReport   `json:"exchange_stage_reports,omitempty"`
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
		rows = append(rows, stateModel.TrafficRow{OperationFamily: trafficOperationFamily(r.ClientFamily, r.Result, r.StatusCode),
			Target:              r.Route,
			Result:              r.Result,
			StatusCode:          r.StatusCode,
			ObservedAt:          r.ObservedAt,
			TTFBMillis:          ttfbMillis,
			DurMillis:           durMillis,
			InputTokens:         inputTokens,
			OutputTokens:        outputTokens,
			CacheReadTokens:     cacheReadTokens,
			CacheWriteTokens:    cacheWriteTokens,
			Mutations:           append([]stateModel.Mutation(nil), r.Mutations...),
			ExchangeDiagnostics: append([]string(nil), r.ExchangeDiagnostics...),
			StageReports:        append([]stateModel.StageReport(nil), r.StageReports...),
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
		if err := validateStageReports(row.StageReports); err != nil {
			return fmt.Errorf("status projection row %d invalid exchange_stage_reports: %w", i, err)
		}
	}
	return nil
}

func validateStageReports(reports []stateModel.StageReport) error {
	if len(reports) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for i, report := range reports {
		stage := strings.TrimSpace(strings.ToLower(report.Stage))     // swobu:io-string source=boundary
		carrier := strings.TrimSpace(strings.ToLower(report.Carrier)) // swobu:io-string source=boundary
		if stage == "" || carrier == "" {
			return fmt.Errorf("entry %d missing stage or carrier", i)
		}
		key := stage + "\x00" + carrier
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate stage/carrier %q/%q", stage, carrier)
		}
		seen[key] = struct{}{}
		if !report.Mutated {
			continue
		}
		appliedCount := 0
		for _, patchID := range report.Applied {
			if strings.TrimSpace(patchID) == "" { // swobu:io-string source=boundary
				continue
			}
			appliedCount++
		}
		if appliedCount == 0 {
			return fmt.Errorf("entry %d mutated without applied patches", i)
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
	Scope            string
	ProviderSpec     string
	BaseURL          string
	AuthHeader       string
	CredentialRef    string
	ProviderProtocol string
}

func (eff LoadRoutingModelCatalogEffect) Execute(ctx context.Context) []update.Action {
	query := url.Values{}
	query.Set("provider_spec", strings.TrimSpace(eff.ProviderSpec)) // swobu:io-string source=boundary
	if baseURL := strings.TrimSpace(eff.BaseURL); baseURL != "" {   // swobu:io-string source=boundary
		query.Set("base_url", baseURL)
	}
	authHeader := strings.TrimSpace(eff.AuthHeader) // swobu:io-string source=boundary
	if authHeader == "" {
		authHeader = stateModel.ProviderDefaultAuthHeader(eff.ProviderSpec)
	}
	if authHeader != "" {
		query.Set("auth_header", authHeader)
	}
	if credentialRef := strings.TrimSpace(eff.CredentialRef); credentialRef != "" { // swobu:io-string source=boundary
		query.Set("credential_ref", credentialRef)
	}
	if providerProtocol := strings.TrimSpace(eff.ProviderProtocol); providerProtocol != "" { // swobu:io-string source=boundary
		query.Set("provider_protocol", providerProtocol)
	}
	type probeResult struct {
		ModelIDs                 []string `json:"model_ids,omitempty"`
		Error                    string   `json:"error,omitempty"`
		ResolvedProviderProtocol string   `json:"resolved_provider_protocol,omitempty"`
	}
	result, err := loadJSONWithTimeout[probeResult](ctx, platformconfig.DefaultDaemonURL()+"/_swobu/model-catalog?"+query.Encode(), modelCatalogProbeLoadTimeout)
	if err != nil {
		normalized := normalizeModelCatalogProbeLoadError(err)
		return []update.Action{RoutingModelCatalogLoaded{
			Scope:            strings.TrimSpace(eff.Scope),        // swobu:io-string source=boundary
			ProviderSpec:     strings.TrimSpace(eff.ProviderSpec), // swobu:io-string source=boundary
			BaseURL:          strings.TrimSpace(eff.BaseURL),      // swobu:io-string source=boundary
			AuthHeader:       authHeader,
			CredentialRef:    strings.TrimSpace(eff.CredentialRef),    // swobu:io-string source=boundary
			ProviderProtocol: strings.TrimSpace(eff.ProviderProtocol), // swobu:io-string source=boundary
			Error:            normalized,
		}}
	}
	return []update.Action{RoutingModelCatalogLoaded{
		Scope:                    strings.TrimSpace(eff.Scope),        // swobu:io-string source=boundary
		ProviderSpec:             strings.TrimSpace(eff.ProviderSpec), // swobu:io-string source=boundary
		BaseURL:                  strings.TrimSpace(eff.BaseURL),      // swobu:io-string source=boundary
		AuthHeader:               authHeader,
		CredentialRef:            strings.TrimSpace(eff.CredentialRef),    // swobu:io-string source=boundary
		ProviderProtocol:         strings.TrimSpace(eff.ProviderProtocol), // swobu:io-string source=boundary
		ModelIDs:                 append([]string(nil), result.ModelIDs...),
		Error:                    strings.TrimSpace(result.Error),                    // swobu:io-string source=boundary
		ResolvedProviderProtocol: strings.TrimSpace(result.ResolvedProviderProtocol), // swobu:io-string source=boundary
	}}
}
