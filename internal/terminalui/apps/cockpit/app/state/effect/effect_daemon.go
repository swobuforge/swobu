// Effect types for outbound async orchestration.
package effect

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/metrofun/swobu/internal/app/operator/controlplane"
	stateModel "github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
)

type daemonRawTrafficRow struct {
	RequestID        string `json:"request_id"`
	ClientHandler    string `json:"client_handler,omitempty"`
	ClientProtocol   string `json:"client_protocol,omitempty"`
	IngressFamily    string `json:"ingress_family,omitempty"`
	NormalizedOp     string `json:"normalized_op,omitempty"`
	Route            string `json:"route"`
	Result           string `json:"result"`
	StatusCode       int    `json:"status_code"`
	ObservedAt       string `json:"observed_at,omitempty"`
	TTFBMillis       *int   `json:"ttfb_millis,omitempty"`
	DurMillis        *int   `json:"dur_millis,omitempty"`
	InputTokens      *int   `json:"input_tokens,omitempty"`
	OutputTokens     *int   `json:"output_tokens,omitempty"`
	CacheReadTokens  *int   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int   `json:"cache_write_tokens,omitempty"`
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
	status, err := loadJSON[daemonStatus](ctx, daemonURL()+"/_swobu/status")
	if err != nil {
		return []update.Action{DaemonStatusLoadFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	if strings.TrimSpace(status.SwobuVersion) == "" {
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
			DaemonVersion:     strings.TrimSpace(status.SwobuVersion),
			HasDaemonProtocol: false,
			Reason:            "status payload is missing required control_plane_protocol",
		}}
	}
	if *status.ControlPlaneProtocol != controlplane.Protocol {
		return []update.Action{ControlPlaneIncompatibleDetected{
			ExpectedProtocol:  controlplane.Protocol,
			DaemonProtocol:    *status.ControlPlaneProtocol,
			TUIVersion:        controlplane.SwobuVersion(),
			DaemonVersion:     strings.TrimSpace(status.SwobuVersion),
			HasDaemonProtocol: true,
			Reason:            "control-plane protocol mismatch",
		}}
	}
	return []update.Action{ReplaceDaemonStatus{
		State:         status.State,
		EndpointCount: status.EndpointCount,
	}}
}

// RefreshCatalogEffect queries the model catalog and reports it back to the UI.
type RefreshCatalogEffect struct{}

func (RefreshCatalogEffect) Execute(ctx context.Context) []update.Action {
	type rawEntry struct {
		EndpointName      string   `json:"endpoint_name"`
		ProviderConfigRef string   `json:"provider_config_ref"`
		ProviderSpec      string   `json:"provider_spec"`
		ProtocolKind      string   `json:"protocol_kind"`
		ModelIDs          []string `json:"model_ids,omitempty"`
		Error             string   `json:"error,omitempty"`
	}
	type catalogResult struct {
		Entries []rawEntry `json:"entries"`
	}
	result, err := loadJSON[catalogResult](ctx, daemonURL()+"/_swobu/model-catalog")
	if err != nil {
		return []update.Action{CatalogLoadFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	entries := make([]stateModel.CatalogEntry, 0, len(result.Entries))
	for _, e := range result.Entries {
		entries = append(entries, stateModel.CatalogEntry{
			EndpointName:      e.EndpointName,
			ProviderConfigRef: e.ProviderConfigRef,
			ProviderSpec:      e.ProviderSpec,
			ProtocolKind:      e.ProtocolKind,
			ModelIDs:          append([]string(nil), e.ModelIDs...),
			Error:             e.Error,
		})
	}
	return []update.Action{ReplaceCatalog{Entries: entries}}
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
	if endpoint := strings.TrimSpace(eff.EndpointName); endpoint != "" {
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
	result, err := loadJSONValidated[statusProjectionDoc](ctx, daemonURL()+"/_swobu/status-projection?"+query.Encode(), func(d statusProjectionDoc) error {
		return validateStatusProjectionDoc(d, requestedScope)
	})
	if err != nil {
		return []update.Action{TrafficLoadFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	rows := make([]stateModel.TrafficRow, 0, len(result.RecentTraffic))
	for _, r := range result.RecentTraffic {
		rows = append(rows, stateModel.TrafficRow{
			RequestID:        r.RequestID,
			OperationFamily:  trafficOperationFamily(r.IngressFamily, r.Result, r.StatusCode),
			Target:           r.Route,
			Result:           r.Result,
			StatusCode:       r.StatusCode,
			ObservedAt:       r.ObservedAt,
			TTFBMillis:       r.TTFBMillis,
			DurMillis:        r.DurMillis,
			InputTokens:      r.InputTokens,
			OutputTokens:     r.OutputTokens,
			CacheReadTokens:  r.CacheReadTokens,
			CacheWriteTokens: r.CacheWriteTokens,
		})
	}
	return []update.Action{ReplaceStatusProjection{Rows: rows}}
}

func validateStatusProjectionDoc(d statusProjectionDoc, requestedScope statusProjectionScope) error {
	if strings.TrimSpace(d.Scope.Kind) == "" {
		return fmt.Errorf("status projection scope is required")
	}
	if d.Scope.Kind != requestedScope.Kind {
		return fmt.Errorf("status projection scope kind mismatch: got %q want %q", d.Scope.Kind, requestedScope.Kind)
	}
	if d.Scope.Kind == "endpoint" && strings.TrimSpace(d.Scope.Endpoint) != requestedScope.Endpoint {
		return fmt.Errorf("status projection scope endpoint mismatch: got %q want %q", d.Scope.Endpoint, requestedScope.Endpoint)
	}
	for i := range d.RecentTraffic {
		row := d.RecentTraffic[i]
		if strings.TrimSpace(row.RequestID) == "" {
			return fmt.Errorf("status projection row %d missing request_id", i)
		}
		if strings.TrimSpace(row.Route) == "" {
			return fmt.Errorf("status projection row %d missing route", i)
		}
		if strings.TrimSpace(row.Result) == "" {
			return fmt.Errorf("status projection row %d missing result", i)
		}
		if strings.TrimSpace(row.ObservedAt) == "" {
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

// CatalogLoadFailed reports that model catalog could not be loaded.
type CatalogLoadFailed struct{ Message string }

// ReplaceCatalog carries the new model catalog.
type ReplaceCatalog struct{ Entries []stateModel.CatalogEntry }

// EndpointsLoadFailed reports that endpoint list could not be loaded.
type EndpointsLoadFailed struct{ Message string }

// ReplaceEndpoints carries the new endpoint list.
type ReplaceEndpoints struct{ Snapshots []stateModel.EndpointSnapshot }

// TrafficLoadFailed reports that traffic projection could not be loaded.
type TrafficLoadFailed struct{ Message string }

// ReplaceStatusProjection carries the new traffic projection.
type ReplaceStatusProjection struct{ Rows []stateModel.TrafficRow }

// LoadCreateDraftModelCatalogEffect queries provider-backed model catalog for
// first-run draft routing state.
type LoadCreateDraftModelCatalogEffect struct {
	ProviderSpec  string
	BaseURL       string
	CredentialRef string
	ProtocolKind  string
}

func (eff LoadCreateDraftModelCatalogEffect) Execute(ctx context.Context) []update.Action {
	query := url.Values{}
	query.Set("provider_spec", strings.TrimSpace(eff.ProviderSpec))
	if baseURL := strings.TrimSpace(eff.BaseURL); baseURL != "" {
		query.Set("base_url", baseURL)
	}
	if credentialRef := strings.TrimSpace(eff.CredentialRef); credentialRef != "" {
		query.Set("credential_ref", credentialRef)
	}
	if protocolKind := strings.TrimSpace(eff.ProtocolKind); protocolKind != "" {
		query.Set("protocol_kind", protocolKind)
	}
	type preview struct {
		ModelIDs []string `json:"model_ids,omitempty"`
		Error    string   `json:"error,omitempty"`
	}
	result, err := loadJSON[preview](ctx, daemonURL()+"/_swobu/model-catalog/preview?"+query.Encode())
	if err != nil {
		normalized := normalizeOperatorSurfaceError(err)
		return []update.Action{CreateDraftModelCatalogLoaded{
			ProviderSpec:  strings.TrimSpace(eff.ProviderSpec),
			BaseURL:       strings.TrimSpace(eff.BaseURL),
			CredentialRef: strings.TrimSpace(eff.CredentialRef),
			Error:         normalized,
		}}
	}
	return []update.Action{CreateDraftModelCatalogLoaded{
		ProviderSpec:  strings.TrimSpace(eff.ProviderSpec),
		BaseURL:       strings.TrimSpace(eff.BaseURL),
		CredentialRef: strings.TrimSpace(eff.CredentialRef),
		ModelIDs:      append([]string(nil), result.ModelIDs...),
		Error:         strings.TrimSpace(result.Error),
	}}
}

// CreateDraftModelCatalogLoaded carries first-run draft model catalog choices.
type CreateDraftModelCatalogLoaded struct {
	ProviderSpec  string
	BaseURL       string
	CredentialRef string
	ModelIDs      []string
	Error         string
}

func normalizeOperatorSurfaceError(err error) string {
	message := strings.TrimSpace(err.Error())
	message = strings.TrimSpace(strings.TrimPrefix(message, "operator client:"))
	if message == "" {
		return daemonUnavailableHint()
	}
	lower := strings.ToLower(message)
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
	return "unavailable at " + daemonURL()
}
