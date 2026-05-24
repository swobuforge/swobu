package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/app/requestpath"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
)

type ephemeralExecuteFunc func(context.Context, endpointintent.Endpoint, requestpath.HandleInput) (requestpath.HandleOutput, error)

type ephemeralExecuteRequest struct {
	Endpoint endpointDocument `json:"endpoint"`
}

type ephemeralExecuteResponse struct {
	ResolvedProviderProtocol string `json:"resolved_provider_protocol,omitempty"`
	Error                    string `json:"error,omitempty"`
}

type EphemeralExecuteHandler struct {
	execute ephemeralExecuteFunc
}

func NewEphemeralExecuteHandler(execute ephemeralExecuteFunc) EphemeralExecuteHandler {
	return EphemeralExecuteHandler{execute: execute}
}

func (h EphemeralExecuteHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.execute == nil {
		http.Error(w, "ephemeral execute unavailable", http.StatusInternalServerError)
		return
	}
	var body ephemeralExecuteRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	endpoint, err := decodeEndpointDocument(body.Endpoint)
	if err != nil {
		http.Error(w, "invalid endpoint", http.StatusBadRequest)
		return
	}
	selected := endpoint.SelectedProviderConfig()
	modelID := strings.TrimSpace(selected.ModelID())
	if modelID == "" {
		http.Error(w, "selected provider model is not configured", http.StatusBadRequest)
		return
	}
	probeReq := canonical.NewDialogRequest(modelID, []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "ping")})
	out, execErr := h.execute(req.Context(), endpoint, requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		Request:      probeReq,
		Contract:     requestpath.NewExecutionContract(false),
		Provenance: requestpath.IngressProvenance{
			ClientProtocol: "_swobu_probe",
			ClientHandler:  "ephemeral_execute",
		},
	})
	if execErr == nil {
		_ = out.Response.Close()
	}
	resp := ephemeralExecuteResponse{
		ResolvedProviderProtocol: selected.ProviderProtocol(),
	}
	if execErr != nil {
		resp.Error = strings.TrimSpace(execErr.Error())
	} else {
		resp.ResolvedProviderProtocol = strings.TrimSpace(out.Target.ProviderProtocol)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
