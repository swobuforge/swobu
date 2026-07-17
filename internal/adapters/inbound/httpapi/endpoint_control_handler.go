// keeps one resource family together at the HTTP edge.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	operatorendpoints "github.com/swobuforge/swobu/internal/app/operator/endpoints"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/exchange"
)

type endpointListFunc func(context.Context) ([]endpointintent.Endpoint, error)
type endpointGetFunc func(context.Context, string) (endpointintent.Endpoint, error)
type endpointPutFunc func(context.Context, endpointintent.Endpoint) (endpointintent.Endpoint, error)
type endpointDeleteFunc func(context.Context, string) error
type endpointAutoProtocolProbeFunc func(context.Context, endpointintent.Endpoint, exchange.RequestInput) (exchange.RequestOutput, error)

const autoProtocolProbeAttemptTimeout = 3 * time.Second

type endpointControlErrorResponse struct {
	Error endpointControlErrorBody `json:"error"`
}

type endpointControlErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type endpointListDocument struct {
	Endpoints []endpointDocument `json:"endpoints"`
}

type endpointDocument struct {
	Name                      string                   `json:"name"`
	SelectedProviderConfigRef string                   `json:"selected_provider_config_ref"`
	ProviderConfigs           []providerConfigDocument `json:"provider_configs"`
}

type providerConfigDocument struct {
	Ref              string `json:"ref"`
	ProviderSpec     string `json:"provider_spec"`
	BaseURL          string `json:"base_url,omitempty"`
	AuthMode         string `json:"auth_mode,omitempty"`
	AuthHeader       string `json:"auth_header,omitempty"`
	CredentialRef    string `json:"credential_ref,omitempty"`
	RouteModelID     string `json:"route_model_id,omitempty"`
	ModelID          string `json:"model_id,omitempty"`
	TargetAlias      string `json:"target_alias,omitempty"`
	TargetRank       *int   `json:"target_rank,omitempty"`
	TargetWeight     *int   `json:"target_weight,omitempty"`
	ProviderProtocol string `json:"provider_protocol,omitempty"`
}

// EndpointControlHandler renders the daemon-owned operator control plane for
// endpoint intent. It stays on the internal `_swobu/*` route family so client
// protocol paths remain separate from operator control.
type EndpointControlHandler struct {
	list   endpointListFunc
	get    endpointGetFunc
	put    endpointPutFunc
	delete endpointDeleteFunc
	probe  endpointAutoProtocolProbeFunc
}

func NewEndpointControlHandler(list endpointListFunc, get endpointGetFunc, put endpointPutFunc, delete endpointDeleteFunc, probe endpointAutoProtocolProbeFunc) EndpointControlHandler {
	return EndpointControlHandler{
		list:   list,
		get:    get,
		put:    put,
		delete: delete,
		probe:  probe,
	}
}

func (h EndpointControlHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	name, hasName := endpointNameFromPath(req.URL.Path)
	switch {
	case isMalformedEndpointControlPath(req.URL.Path):
		http.NotFound(w, req)
		return
	case !hasName:
		h.serveCollection(w, req)
	default:
		h.serveResource(w, req, name)
	}
}

func (h EndpointControlHandler) serveCollection(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.list == nil {
		writeEndpointControlError(w, operatorendpoints.CommandError{
			Code:    operatorendpoints.CommandUnavailable,
			Message: "endpoint control plane is unavailable",
		})
		return
	}
	endpoints, err := h.list(req.Context())
	if err != nil {
		writeEndpointControlError(w, err)
		return
	}
	doc := endpointListDocument{Endpoints: make([]endpointDocument, 0, len(endpoints))}
	for _, endpoint := range endpoints {
		doc.Endpoints = append(doc.Endpoints, encodeEndpointDocument(endpoint))
	}
	writeEndpointControlJSON(w, http.StatusOK, doc)
}

// preserving structured daemon-side operator errors.
func (h EndpointControlHandler) serveResource(w http.ResponseWriter, req *http.Request, name string) {
	if req.Method == http.MethodGet {
		if h.get == nil {
			writeEndpointControlError(w, operatorendpoints.CommandError{
				Code:    operatorendpoints.CommandUnavailable,
				Message: "endpoint control plane is unavailable",
			})
			return
		}
		endpoint, err := h.get(req.Context(), name)
		if err != nil {
			writeEndpointControlError(w, err)
			return
		}
		writeEndpointControlJSON(w, http.StatusOK, encodeEndpointDocument(endpoint))
		return
	}
	if req.Method == http.MethodPut {
		if h.put == nil {
			writeEndpointControlError(w, operatorendpoints.CommandError{
				Code:    operatorendpoints.CommandUnavailable,
				Message: "endpoint control plane is unavailable",
			})
			return
		}
		var doc endpointDocument
		if err := json.NewDecoder(req.Body).Decode(&doc); err != nil {
			writeEndpointControlError(w, operatorendpoints.CommandError{
				Code:    operatorendpoints.CommandInvalidArgument,
				Message: "endpoint document could not be decoded",
				Err:     err,
			})
			return
		}
		if strings.TrimSpace(doc.Name) != strings.TrimSpace(name) { // swobu:io-string source=boundary
			writeEndpointControlError(w, operatorendpoints.CommandError{
				Code:    operatorendpoints.CommandInvalidArgument,
				Message: "endpoint document name must match the request path",
			})
			return
		}
		endpoint, err := decodeEndpointDocument(doc)
		if err != nil {
			writeEndpointControlError(w, operatorendpoints.CommandError{
				Code:    operatorendpoints.CommandInvalidArgument,
				Message: err.Error(),
				Err:     err,
			})
			return
		}
		endpoint, err = resolveAutoProviderProtocols(req.Context(), endpoint, doc, h.probe)
		if err != nil {
			writeEndpointControlError(w, operatorendpoints.CommandError{
				Code:    operatorendpoints.CommandInvalidArgument,
				Message: err.Error(),
				Err:     err,
			})
			return
		}
		saved, err := h.put(req.Context(), endpoint)
		if err != nil {
			writeEndpointControlError(w, err)
			return
		}
		writeEndpointControlJSON(w, http.StatusOK, encodeEndpointDocument(saved))
		return
	}
	if req.Method == http.MethodDelete {
		if h.delete == nil {
			writeEndpointControlError(w, operatorendpoints.CommandError{
				Code:    operatorendpoints.CommandUnavailable,
				Message: "endpoint control plane is unavailable",
			})
			return
		}
		if err := h.delete(req.Context(), name); err != nil {
			writeEndpointControlError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func endpointNameFromPath(path string) (string, bool) {
	if path == "/_swobu/endpoints" || path == "/_swobu/endpoints/" {
		return "", false
	}
	if !strings.HasPrefix(path, "/_swobu/endpoints/") {
		return "", false
	}
	name := strings.TrimPrefix(path, "/_swobu/endpoints/")
	if strings.Contains(name, "/") || strings.TrimSpace(name) == "" { // swobu:io-string source=boundary
		return "", false
	}
	return name, true
}

func isMalformedEndpointControlPath(path string) bool {
	if path == "/_swobu/endpoints" || path == "/_swobu/endpoints/" {
		return false
	}
	if !strings.HasPrefix(path, "/_swobu/endpoints/") {
		return false
	}
	name := strings.TrimPrefix(path, "/_swobu/endpoints/")
	return strings.Contains(name, "/") || strings.TrimSpace(name) == "" // swobu:io-string source=boundary
}
