package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/swobuforge/swobu/internal/search"
)

// NewSearchEvidenceHandler projects bounded, content-free direct Search facts
// to the trusted local operator plane. It never exposes query text or results.
func NewSearchEvidenceHandler(store *search.EvidenceStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !IsLocalOperatorRequest(r) {
			http.Error(w, "trusted local operator context required", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			Events         []search.Evidence `json:"events"`
			RetentionError bool              `json:"retention_error"`
		}{Events: store.Events(), RetentionError: store.RetentionError()})
	})
}
