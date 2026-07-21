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
	Deployments              []profile.ProviderDeploymentRecord `json:"deployments,omitempty"`
	Error                    string                             `json:"error,omitempty"`
	ResolvedProviderProtocol string                             `json:"resolved_provider_protocol,omitempty"`
	Diagnostics              json.RawMessage                    `json:"diagnostics,omitempty"`
}

type targetProbeRequest struct {
	Connection       workspaceapi.Connection `json:"connection"`
	ProviderProtocol string                  `json:"provider_protocol,omitempty"`
}

// TargetProbeHandler probes provider-backed deployments for one draft route.
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
	connection, err := input.Connection.RoutingConnection()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	providerSpec := string(connection.Provider())
	result := TargetProbeResult{}
	probe, resolvedVariant, probeErr := probeDeployments(req.Context(), h.providers, connection, input.ProviderProtocol)
	if probeErr != nil {
		slog.Warn("model catalog probe failed",
			"provider_spec", providerSpec,
			"provider_protocol", input.ProviderProtocol,
			"error", probeErr.Error(),
		)
		result.Error = normalizeModelCatalogProbeError(probeErr.Error(), connectionCredentialRef(connection))
		result.Diagnostics = json.RawMessage(probe.Diagnostics)
	} else {
		slog.Debug("model catalog probe succeeded",
			"provider_spec", providerSpec,
			"provider_protocol", resolvedVariant,
			"deployment_count", len(probe.Deployments),
		)
		result.Deployments = probe.Deployments
		result.Diagnostics = json.RawMessage(probe.Diagnostics)
		result.ResolvedProviderProtocol = resolvedVariant
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
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
	providerProtocol = strings.TrimSpace(providerProtocol) // swobu:io-string source=boundary
	variants := modelCatalogProbeVariants(providerSpec, providerProtocol)
	var lastErr error
	var lastProbe provider.TargetProbeResult
	for _, variant := range variants {
		protocol, frame, ok := profile.ProviderProtocolKindAndFrame(providerSpec, variant)
		if !ok {
			continue
		}
		_ = protocol
		_ = frame
		target, err := exchange.ProviderTargetFromConnection("draft", connection, variant)
		if err != nil {
			lastErr = err
			continue
		}
		probe, err := providers.ProbeTarget(ctx, target)
		if err == nil {
			probe.Deployments = profile.CloneProviderDeployments(probe.Deployments)
			return probe, variant, nil
		}
		lastErr = err
		lastProbe = probe
	}
	if lastErr != nil {
		return lastProbe, "", lastErr
	}
	return provider.TargetProbeResult{}, "", canonical.BadEndpoint("selected provider route is unsupported")
}

func connectionCredentialRef(connection routing.Connection) string {
	switch c := connection.(type) {
	case routing.OpenAIConnection:
		return c.Credential().String()
	case routing.AnthropicConnection:
		return c.Credential().String()
	case routing.OpenRouterConnection:
		return c.Credential().String()
	case routing.ChatGPTConnection:
		return c.Credential().String()
	case routing.AzureConnection:
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

func modelCatalogProbeVariants(providerSpec string, providerProtocol string) []string {
	supported := profile.ConcreteProviderProtocolsForSpec(providerSpec)
	variants := make([]string, 0, len(supported))
	seen := map[string]struct{}{}
	appendVariant := func(variant string) {
		variant = strings.TrimSpace(variant) // swobu:io-string source=boundary
		if variant == "" {
			return
		}
		if _, exists := seen[variant]; exists {
			return
		}
		seen[variant] = struct{}{}
		variants = append(variants, variant)
	}
	appendVariant(providerProtocol)
	for _, variant := range supported {
		appendVariant(variant)
	}
	return variants
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
