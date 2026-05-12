package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
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
	providerSpec := strings.TrimSpace(strings.ToLower(query.Get("provider_spec")))
	if providerSpec == "" {
		http.Error(w, "provider_spec is required", http.StatusBadRequest)
		return
	}
	protocolKindRaw := strings.TrimSpace(query.Get("protocol_kind"))
	if protocolKindRaw == "" {
		http.Error(w, "protocol_kind is required", http.StatusBadRequest)
		return
	}
	protocolKind, err := protocolsurface.Parse(protocolKindRaw)
	if err != nil {
		http.Error(w, "protocol_kind is invalid", http.StatusBadRequest)
		return
	}
	baseURL := strings.TrimSpace(query.Get("base_url"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(providercatalog.DefaultBaseURL(providerSpec))
	}
	credentialRef := strings.TrimSpace(query.Get("credential_ref"))

	models, probeErr := probeModelIDs(req.Context(), h.providers, providerSpec, baseURL, credentialRef, protocolKind)
	result := modelCatalogProbeResult{}
	if probeErr != nil {
		result.Error = normalizeModelCatalogProbeError(probeErr.Error(), credentialRef)
	} else {
		result.ModelIDs = models
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func probeModelIDs(ctx context.Context, providers ports.ProviderModelCatalog, providerSpec string, baseURL string, credentialRef string, protocolKind protocolsurface.Kind) ([]string, error) {
	routeProfile, ok := providercatalog.ResolveRouteProfile(providerSpec, protocolKind, baseURL, credentialRef)
	if !ok {
		return nil, compatibility.BadEndpoint("selected provider route is unsupported")
	}
	models, err := providers.ListModels(ctx, ports.NewRoutableTarget(
		"draft",
		providerSpec,
		baseURL,
		credentialRef,
		protocolKind,
		string(routeProfile.AuthKind),
		string(routeProfile.APIFamily),
	))
	if err != nil {
		return nil, err
	}
	return ports.CloneModelIDs(models), nil
}

func normalizeModelCatalogProbeError(message string, credentialRef string) string {
	message = strings.TrimSpace(message)
	if !strings.Contains(strings.ToLower(message), "credential reference could not be resolved") {
		return message
	}
	if !isFileCredentialRef(credentialRef) {
		return message
	}
	return "BAD_ENDPOINT: credential file could not be resolved (check file path, read permission, and non-empty token)"
}

func isFileCredentialRef(credentialRef string) bool {
	ref := strings.TrimSpace(strings.ToLower(credentialRef))
	return ref == "file" ||
		ref == "file:" ||
		strings.HasPrefix(ref, "file:") ||
		strings.HasPrefix(ref, "/") ||
		strings.HasPrefix(ref, "~/")
}
