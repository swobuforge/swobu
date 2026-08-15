package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

type TargetProbeResult struct {
	Options                  []profile.ModelAuthoringOption `json:"deployments,omitempty"`
	Error                    string                         `json:"error,omitempty"`
	ResolvedProviderProtocol string                         `json:"resolved_provider_protocol,omitempty"`
	Diagnostics              json.RawMessage                `json:"diagnostics,omitempty"`
}

type targetProbeRequest struct {
	Connection       workspaceapi.Connection `json:"connection"`
	ProviderProtocol string                  `json:"provider_protocol,omitempty"`
}

// TargetProbeHandler probes provider-backed model-authoring options for one
// draft route. The response keeps the established "deployments" JSON member
// as a compatibility boundary.
type TargetProbeHandler struct {
	providers provider.Discovery
}

func NewTargetProbeHandler(providers provider.Discovery) TargetProbeHandler {
	return TargetProbeHandler{providers: providers}
}

func (h TargetProbeHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.providers == nil {
		http.Error(w, "model catalog unavailable", http.StatusInternalServerError)
		return
	}

	var input targetProbeRequest
	if err := decodeOperatorJSONObject(w, req, &input, "target probe request"); err != nil {
		http.Error(w, "target probe request is invalid", http.StatusBadRequest)
		return
	}
	var connection routing.Connection
	providerSpec := ""
	credentialRef := ""
	bedrock, isBedrock := input.Connection.BedrockDraft()
	if isBedrock && strings.TrimSpace(bedrock.Endpoint) == "" {
		providerSpec = string(profile.ProviderSpecBedrock)
		credentialRef = strings.TrimSpace(bedrock.Credential)
	} else {
		var err error
		connection, err = input.Connection.RoutingConnection()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		providerSpec = string(connection.Provider())
		credentialRef = connectionCredentialRef(connection)
	}
	result := TargetProbeResult{}
	var probe provider.TargetProbeResult
	var resolvedVariant string
	var probeErr error
	if isBedrock && strings.TrimSpace(bedrock.Endpoint) == "" {
		probe, resolvedVariant, probeErr = probeBedrockCatalog(
			req.Context(), h.providers, bedrock, input.ProviderProtocol,
		)
	} else {
		probe, resolvedVariant, probeErr = probeDeployments(req.Context(), h.providers, connection, input.ProviderProtocol)
	}
	if probeErr != nil {
		slog.Warn("model catalog probe failed",
			"provider_spec", providerSpec,
			"provider_protocol", input.ProviderProtocol,
			"error", probeErr.Error(),
		)
		result.Error = normalizeModelCatalogProbeError(probeErr.Error(), credentialRef)
		result.Diagnostics = json.RawMessage(probe.Diagnostics)
	} else {
		slog.Debug("model catalog probe succeeded",
			"provider_spec", providerSpec,
			"provider_protocol", resolvedVariant,
			"model_option_count", len(probe.Options),
		)
		result.Options = probe.Options
		result.Diagnostics = json.RawMessage(probe.Diagnostics)
		result.ResolvedProviderProtocol = resolvedVariant
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func probeBedrockCatalog(
	ctx context.Context,
	providers provider.Discovery,
	connection workspaceapi.BedrockConnection,
	providerProtocol string,
) (provider.TargetProbeResult, string, error) {
	region, err := routing.ParseBedrockRegion(connection.Region)
	if err != nil {
		return provider.TargetProbeResult{}, "", err
	}
	variant, protocol, err := selectModelCatalogProtocol(string(profile.ProviderSpecBedrock), providerProtocol)
	if err != nil {
		return provider.TargetProbeResult{}, "", err
	}
	target := provider.NewBedrockTargetSnapshot(
		"draft", "", strings.TrimSpace(connection.Credential), protocol.Kind,
		variant, region.String(), protocol.Delivery)
	probe, probeErr := providers.ProbeTarget(ctx, target)
	if probeErr != nil {
		return probe, "", probeErr
	}
	probe.Options = profile.CloneModelAuthoringOptions(probe.Options)
	return probe, variant, nil
}

func credentialRefKindForProbe(credentialRef string) string {
	return string(credentialref.Parse(credentialRef).Kind())
}

func probeDeployments(
	ctx context.Context,
	providers provider.Discovery,
	connection routing.Connection,
	providerProtocol string,
) (provider.TargetProbeResult, string, error) {
	providerSpec := string(connection.Provider())
	if !profile.SupportsSpec(providerSpec) {
		return provider.TargetProbeResult{}, "", canonical.BadEndpoint("selected provider route is unsupported")
	}
	variant, _, err := selectModelCatalogProtocol(providerSpec, providerProtocol)
	if err != nil {
		return provider.TargetProbeResult{}, "", err
	}
	target, err := exchange.ProviderTargetFromConnection("draft", connection, variant)
	if err != nil {
		return provider.TargetProbeResult{}, "", err
	}
	probe, probeErr := providers.ProbeTarget(ctx, target)
	if probeErr != nil {
		return probe, "", probeErr
	}
	probe.Options = profile.CloneModelAuthoringOptions(probe.Options)
	return probe, variant, nil
}

func connectionCredentialRef(connection routing.Connection) string {
	switch c := connection.(type) {
	case routing.StandardConnection:
		return c.Credential().String()
	case routing.ZAIConnection:
		return c.Credential().String()
	case routing.BedrockConnection:
		return c.Credential().String()
	case routing.CustomConnection:
		if auth, ok := c.Auth().(routing.CustomHeaderAuth); ok {
			return auth.Credential().String()
		}
	}
	return ""
}

// selectModelCatalogProtocol chooses one static concrete contract for an
// advisory authoring probe. An authored exact contract wins; otherwise the
// first manifest entry supplies the static preference. Probe failure is
// returned to the operator and never triggers a second protocol attempt.
func selectModelCatalogProtocol(providerSpec string, authored string) (string, profile.ProviderProtocolSpec, error) {
	selected := strings.TrimSpace(authored) // swobu:io-string source=boundary
	if selected == "" {
		protocols := profile.ConcreteProviderProtocolsForSpec(providerSpec)
		if len(protocols) > 0 {
			selected = protocols[0]
		}
	}
	normalized, err := profile.NormalizeProviderProtocolForSpec(providerSpec, selected)
	if err != nil || normalized == "" {
		if err != nil {
			return "", profile.ProviderProtocolSpec{}, canonical.BadEndpoint(err.Error())
		}
		return "", profile.ProviderProtocolSpec{}, canonical.BadEndpoint("selected provider route is unsupported")
	}
	spec, ok := profile.ProviderProtocolSpecForSpec(providerSpec, normalized)
	if !ok {
		return "", profile.ProviderProtocolSpec{}, canonical.BadEndpoint("selected provider route is unsupported")
	}
	return normalized, spec, nil
}

func normalizeModelCatalogProbeError(message string, credentialRef string) string {
	message = strings.TrimSpace(message)                                                           // swobu:io-string source=boundary
	if !strings.Contains(strings.ToLower(message), "credential reference could not be resolved") { // swobu:io-string source=boundary
		return message
	}
	if !credentialref.Parse(credentialRef).IsFileRef() {
		return message
	}
	return "BAD_ENDPOINT: credential file could not be resolved (check file path, read permission, and non-empty token)"
}
