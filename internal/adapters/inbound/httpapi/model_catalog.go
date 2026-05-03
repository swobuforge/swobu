package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	operatormodelcatalog "github.com/swobuforge/swobu/internal/app/operator/modelcatalog"
)

type modelCatalogReadFunc func(context.Context) (operatormodelcatalog.Snapshot, error)

// ModelCatalogHandler renders the daemon-owned operator model catalog read
// model. It stays on the operator-support route family rather than the client
// compatibility contract.
type ModelCatalogHandler struct {
	read modelCatalogReadFunc
}

func NewModelCatalogHandler(read modelCatalogReadFunc) ModelCatalogHandler {
	return ModelCatalogHandler{read: read}
}

func (h ModelCatalogHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.read == nil {
		http.Error(w, "model catalog unavailable", http.StatusInternalServerError)
		return
	}
	snapshot, err := h.read(req.Context())
	if err != nil {
		http.Error(w, "model catalog unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snapshot)
}
