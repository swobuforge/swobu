// keeps one resource family together at the HTTP edge.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	operatorendpoints "github.com/swobuforge/swobu/internal/app/operator/endpoints"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
)

type endpointListFunc func(context.Context) ([]endpointintent.Endpoint, error)
type endpointGetFunc func(context.Context, string) (endpointintent.Endpoint, error)
type endpointPutFunc func(context.Context, endpointintent.Endpoint) (endpointintent.Endpoint, error)
type endpointDeleteFunc func(context.Context, string) error

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
	Ref           string `json:"ref"`
	ProviderSpec  string `json:"provider_spec"`
	BaseURL       string `json:"base_url,omitempty"`
	CredentialRef string `json:"credential_ref,omitempty"`
	ModelID       string `json:"model_id,omitempty"`
	TargetAlias   string `json:"target_alias,omitempty"`
	ProtocolKind  string `json:"protocol_kind"`
}

// EndpointControlHandler renders the daemon-owned operator control plane for
// endpoint intent. It stays on the internal `_swobu/*` route family so client
// compatibility paths remain separate from operator control.
type EndpointControlHandler struct {
	list   endpointListFunc
	get    endpointGetFunc
	put    endpointPutFunc
	delete endpointDeleteFunc
}

func NewEndpointControlHandler(list endpointListFunc, get endpointGetFunc, put endpointPutFunc, delete endpointDeleteFunc) EndpointControlHandler {
	return EndpointControlHandler{
		list:   list,
		get:    get,
		put:    put,
		delete: delete,
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
	switch req.Method {
	case http.MethodGet:
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
	case http.MethodPut:
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
		if strings.TrimSpace(doc.Name) != strings.TrimSpace(name) {
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
		saved, err := h.put(req.Context(), endpoint)
		if err != nil {
			writeEndpointControlError(w, err)
			return
		}
		writeEndpointControlJSON(w, http.StatusOK, encodeEndpointDocument(saved))
	case http.MethodDelete:
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
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func endpointNameFromPath(path string) (string, bool) {
	switch path {
	case "/_swobu/endpoints", "/_swobu/endpoints/":
		return "", false
	}
	if !strings.HasPrefix(path, "/_swobu/endpoints/") {
		return "", false
	}
	name := strings.TrimPrefix(path, "/_swobu/endpoints/")
	if strings.Contains(name, "/") || strings.TrimSpace(name) == "" {
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
	return strings.Contains(name, "/") || strings.TrimSpace(name) == ""
}

func encodeEndpointDocument(endpoint endpointintent.Endpoint) endpointDocument {
	providerConfigs := endpoint.ProviderConfigs()
	doc := endpointDocument{
		Name:                      endpoint.Name().String(),
		SelectedProviderConfigRef: endpoint.SelectedProviderConfigRef().String(),
		ProviderConfigs:           make([]providerConfigDocument, 0, len(providerConfigs)),
	}
	for _, providerConfig := range providerConfigs {
		doc.ProviderConfigs = append(doc.ProviderConfigs, providerConfigDocument{
			Ref:           providerConfig.Ref().String(),
			ProviderSpec:  providerConfig.ProviderSpec().String(),
			BaseURL:       providerConfig.BaseURL(),
			CredentialRef: providerConfig.CredentialRef(),
			ModelID:       providerConfig.ModelID(),
			TargetAlias:   providerConfig.TargetAlias(),
			ProtocolKind:  providerConfig.ProtocolKind().String(),
		})
	}
	return doc
}

func decodeEndpointDocument(doc endpointDocument) (endpointintent.Endpoint, error) {
	name, err := endpointintent.ParseEndpointName(doc.Name)
	if err != nil {
		return endpointintent.Endpoint{}, err
	}
	selectedRef, err := endpointintent.ParseProviderConfigRef(doc.SelectedProviderConfigRef)
	if err != nil {
		return endpointintent.Endpoint{}, err
	}
	providerConfigs := make([]endpointintent.ProviderConfig, 0, len(doc.ProviderConfigs))
	for _, encoded := range doc.ProviderConfigs {
		ref, err := endpointintent.ParseProviderConfigRef(encoded.Ref)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		spec, err := endpointintent.ParseProviderSpec(encoded.ProviderSpec)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		providerConfig, err := endpointintent.NewProviderConfig(ref, spec, encoded.BaseURL, encoded.CredentialRef, protocolsurface.Kind(encoded.ProtocolKind))
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		providerConfig, err = providerConfig.WithModelID(encoded.ModelID)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		providerConfig, err = providerConfig.WithTargetAlias(encoded.TargetAlias)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		providerConfigs = append(providerConfigs, providerConfig)
	}
	return endpointintent.NewEndpoint(name, providerConfigs, selectedRef)
}

func writeEndpointControlJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeEndpointControlError(w http.ResponseWriter, err error) {
	var commandErr operatorendpoints.CommandError
	if !errors.As(err, &commandErr) {
		commandErr = operatorendpoints.CommandError{
			Code:    operatorendpoints.CommandInternal,
			Message: "endpoint control plane failed",
			Err:     err,
		}
	}
	writeEndpointControlJSON(w, statusCodeForEndpointControlError(commandErr.Code), endpointControlErrorResponse{
		Error: endpointControlErrorBody{
			Code:    string(commandErr.Code),
			Message: commandErr.Error(),
		},
	})
}

func statusCodeForEndpointControlError(code operatorendpoints.CommandErrorCode) int {
	switch code {
	case operatorendpoints.CommandInvalidArgument:
		return http.StatusBadRequest
	case operatorendpoints.CommandNotFound:
		return http.StatusNotFound
	case operatorendpoints.CommandConflict:
		return http.StatusConflict
	case operatorendpoints.CommandUnavailable:
		return 503
	default:
		return http.StatusInternalServerError
	}
}
