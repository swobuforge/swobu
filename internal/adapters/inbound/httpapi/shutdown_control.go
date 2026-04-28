package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
)

type shutdownRequestFunc func(context.Context) error

type shutdownResponse struct {
	OK bool `json:"ok"`
}

type ShutdownHandler struct {
	request shutdownRequestFunc
}

func NewShutdownHandler(request shutdownRequestFunc) ShutdownHandler {
	return ShutdownHandler{request: request}
}

func (h ShutdownHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.request == nil {
		http.Error(w, "shutdown unavailable", http.StatusInternalServerError)
		return
	}
	if err := h.request(req.Context()); err != nil {
		http.Error(w, "shutdown unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(shutdownResponse{OK: true})
}
