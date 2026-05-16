package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

type modelCatalogProbeResult struct {
	ModelIDs []string `json:"model_ids,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// ModelCatalogProbeHandler probes provider-backed model ids for one draft route.
type ModelCatalogProbeHandler struct {
	providers ports.ProviderModelCatalog
}

func NewModelCatalogProbeHandler(providers ports.ProviderModelCatalog) ModelCatalogProbeHandler {
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
	providerSpec := strings.TrimSpace(strings.ToLower(query.Get("provider_spec"))) // trimlowerlint:allow boundary canonicalization
	if providerSpec == "" {
		http.Error(w, "provider_spec is required", http.StatusBadRequest)
		return
	}
	baseURL := strings.TrimSpace(query.Get("base_url")) // trimlowerlint:allow boundary canonicalization
	if baseURL == "" {
		baseURL = strings.TrimSpace(providercatalog.DefaultExecuteBaseURL(providerSpec)) // trimlowerlint:allow boundary canonicalization
	}
	credentialRef := strings.TrimSpace(query.Get("credential_ref")) // trimlowerlint:allow boundary canonicalization

	models, probeErr := probeModelIDs(req.Context(), h.providers, providerSpec, baseURL, credentialRef)
	result := modelCatalogProbeResult{}
	if probeErr != nil {
		slog.Warn("model catalog probe failed",
			"provider_spec", providerSpec,
			"base_url", baseURL,
			"credential_ref_kind", credentialRefKindForProbe(credentialRef),
			"error", probeErr.Error(),
		)
		result.Error = normalizeModelCatalogProbeError(probeErr.Error(), credentialRef)
	} else {
		slog.Debug("model catalog probe succeeded",
			"provider_spec", providerSpec,
			"base_url", baseURL,
			"credential_ref_kind", credentialRefKindForProbe(credentialRef),
			"model_count", len(models),
		)
		result.ModelIDs = models
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func credentialRefKindForProbe(credentialRef string) string {
	return string(credentialref.Parse(credentialRef).Kind())
}

func probeModelIDs(ctx context.Context, providers ports.ProviderModelCatalog, providerSpec string, baseURL string, credentialRef string) ([]string, error) {
	routeProfile, ok := providercatalog.ResolveRouteProfile(providerSpec, baseURL, credentialRef)
	if !ok {
		return nil, canonical.BadEndpoint("selected provider route is unsupported")
	}
	models, err := providers.ListModels(ctx, ports.NewRoutableTarget(
		"draft",
		providerSpec,
		baseURL,
		credentialRef,
		"",
		string(routeProfile.AuthKind),
	))
	if err != nil {
		return nil, err
	}
	return ports.CloneModelIDs(models), nil
}

func normalizeModelCatalogProbeError(message string, credentialRef string) string {
	message = strings.TrimSpace(message)                                                           // trimlowerlint:allow boundary canonicalization
	if !strings.Contains(strings.ToLower(message), "credential reference could not be resolved") { // trimlowerlint:allow boundary canonicalization
		return message
	}
	if !credentialref.Parse(credentialRef).IsFileRef() {
		return message
	}
	return "BAD_ENDPOINT: credential file could not be resolved (check file path, read permission, and non-empty token)"
}
