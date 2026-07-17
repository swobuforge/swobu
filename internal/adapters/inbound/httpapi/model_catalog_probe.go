package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
)

type modelCatalogProbeResult struct {
	Deployments              []profile.ProviderDeploymentRecord `json:"deployments,omitempty"`
	Error                    string                             `json:"error,omitempty"`
	ResolvedProviderProtocol string                             `json:"resolved_provider_protocol,omitempty"`
}

// ModelCatalogProbeHandler probes provider-backed deployments for one draft route.
type ModelCatalogProbeHandler struct {
	providers exchange.ProviderModelCatalog
}

func NewModelCatalogProbeHandler(providers exchange.ProviderModelCatalog) ModelCatalogProbeHandler {
	return ModelCatalogProbeHandler{providers: providers}
}

func (h ModelCatalogProbeHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.providers == nil {
		http.Error(w, "model catalog unavailable", http.StatusInternalServerError)
		return
	}

	query := req.URL.Query()
	providerSpec := strings.TrimSpace(strings.ToLower(query.Get("provider_spec"))) // swobu:io-string source=boundary
	if providerSpec == "" {
		http.Error(w, "provider_spec is required", http.StatusBadRequest)
		return
	}
	baseURL := strings.TrimSpace(query.Get("base_url")) // swobu:io-string source=boundary
	if baseURL == "" {
		baseURL = strings.TrimSpace(profile.DefaultExecuteBaseURL(providerSpec)) // swobu:io-string source=boundary
	}
	if baseURL == "" && profile.RequiresExplicitEndpoint(providerSpec) {
		label := profile.EndpointLabelForProvider(providerSpec)
		if label == "" {
			label = "endpoint"
		}
		http.Error(w, label+" is required for provider "+providerSpec, http.StatusBadRequest)
		return
	}
	authHeader := strings.TrimSpace(query.Get("auth_header"))             // swobu:io-string source=boundary
	credentialRef := strings.TrimSpace(query.Get("credential_ref"))       // swobu:io-string source=boundary
	authMode := strings.TrimSpace(query.Get("auth_mode"))                 // swobu:io-string source=boundary
	providerProtocol := strings.TrimSpace(query.Get("provider_protocol")) // swobu:io-string source=boundary
	deployments, resolvedVariant, probeErr := probeDeployments(req.Context(), h.providers, providerSpec, baseURL, authHeader, credentialRef, authMode, providerProtocol)
	result := modelCatalogProbeResult{}
	if probeErr != nil {
		slog.Warn("model catalog probe failed",
			"provider_spec", providerSpec,
			"base_url", baseURL,
			"auth_header", authHeader,
			"credential_ref_kind", credentialRefKindForProbe(credentialRef),
			"auth_mode", authMode,
			"provider_protocol", providerProtocol,
			"error", probeErr.Error(),
		)
		result.Error = normalizeModelCatalogProbeError(probeErr.Error(), credentialRef)
	} else {
		slog.Debug("model catalog probe succeeded",
			"provider_spec", providerSpec,
			"base_url", baseURL,
			"auth_header", authHeader,
			"credential_ref_kind", credentialRefKindForProbe(credentialRef),
			"auth_mode", authMode,
			"provider_protocol", resolvedVariant,
			"deployment_count", len(deployments),
		)
		result.Deployments = deployments
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
	providers exchange.ProviderModelCatalog,
	providerSpec string,
	baseURL string,
	authHeader string,
	credentialRef string,
	authMode string,
	providerProtocol string,
) ([]profile.ProviderDeploymentRecord, string, error) {
	routeProfile, ok := profile.ResolveRouteProfile(providerSpec, baseURL, credentialRef)
	if !ok {
		return nil, "", canonical.BadEndpoint("selected provider route is unsupported")
	}
	providerProtocol = strings.TrimSpace(providerProtocol) // swobu:io-string source=boundary
	variants := modelCatalogProbeVariants(providerSpec, providerProtocol)
	var lastErr error
	for _, variant := range variants {
		protocol, frame, ok := profile.ProviderProtocolKindAndFrame(providerSpec, variant)
		if !ok {
			continue
		}
		target := exchange.NewRoutableTarget(
			"draft",
			providerSpec,
			baseURL,
			credentialRef,
			protocol,
			string(routeProfile.AuthKind),
			frame,
			variant,
		)
		target.AuthHeader = strings.TrimSpace(authHeader) // swobu:io-string source=boundary
		target.AuthMode = strings.TrimSpace(authMode)     // swobu:io-string source=boundary
		deployments, err := providers.ListDeployments(ctx, target)
		if err == nil {
			return profile.CloneProviderDeployments(deployments), variant, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", canonical.BadEndpoint("selected provider route is unsupported")
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
