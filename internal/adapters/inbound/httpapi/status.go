package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
)

// StatusDocument is the minimal machine-readable daemon status payload exposed
// at the HTTP edge. It should stay small and reflect one authoritative health
// field rather than growing decorative operability detail.
type StatusDocument struct {
	State                string `json:"state"`
	EndpointCount        int    `json:"endpoint_count"`
	ControlPlaneProtocol int    `json:"control_plane_protocol"`
	SwobuVersion         string `json:"swobu_version"`
}

type statusReadFunc func(context.Context) (StatusDocument, error)

type StatusHandler struct {
	read statusReadFunc
}

func NewStatusHandler(read statusReadFunc) StatusHandler {
	return StatusHandler{read: read}
}

func (h StatusHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.read == nil {
		http.Error(w, "status unavailable", http.StatusInternalServerError)
		return
	}
	status, err := h.read(req.Context())
	if err != nil {
		http.Error(w, "status unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}
