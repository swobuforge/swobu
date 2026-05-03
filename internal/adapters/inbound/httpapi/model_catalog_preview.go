package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	operatormodelcatalog "github.com/swobuforge/swobu/internal/app/operator/modelcatalog"
)

type modelCatalogPreviewReadFunc func(context.Context, operatormodelcatalog.PreviewRequest) (operatormodelcatalog.PreviewSnapshot, error)

// ModelCatalogPreviewHandler renders provider-backed model catalog choices for
// first-run draft routing state.
type ModelCatalogPreviewHandler struct {
	read modelCatalogPreviewReadFunc
}

func NewModelCatalogPreviewHandler(read modelCatalogPreviewReadFunc) ModelCatalogPreviewHandler {
	return ModelCatalogPreviewHandler{read: read}
}

func (h ModelCatalogPreviewHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.read == nil {
		http.Error(w, "model catalog unavailable", http.StatusInternalServerError)
		return
	}
	query := req.URL.Query()
	providerSpec := strings.TrimSpace(query.Get("provider_spec"))
	if providerSpec == "" {
		http.Error(w, "provider_spec is required", http.StatusBadRequest)
		return
	}
	snapshot, err := h.read(req.Context(), operatormodelcatalog.PreviewRequest{
		ProviderSpec:  providerSpec,
		BaseURL:       strings.TrimSpace(query.Get("base_url")),
		CredentialRef: strings.TrimSpace(query.Get("credential_ref")),
		ProtocolKind:  strings.TrimSpace(query.Get("protocol_kind")),
	})
	if err != nil {
		http.Error(w, "model catalog unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snapshot)
}
