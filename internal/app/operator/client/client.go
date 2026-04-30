// Shared HTTP client for the daemon operator control plane.
// All operator clients (TUI, CLI, WebUI) should use this package
// rather than issuing raw HTTP requests.
package operatorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
)

// Client talks to the daemon's operator control plane at /_swobu/endpoints.
type Client struct {
	http    *http.Client
	baseURL string
}

type AccessCheckResult struct {
	Status  string
	Message string
}

// New creates a client that talks to the daemon at the given base URL
// (e.g. "http://127.0.0.1:9876").
func New(httpClient *http.Client, baseURL string) *Client {
	return &Client{http: httpClient, baseURL: strings.TrimRight(baseURL, "/")}
}

// List returns all endpoint intents from the daemon.
func (c *Client) List(ctx context.Context) ([]endpointintent.Endpoint, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_swobu/endpoints", nil)
	if err != nil {
		return nil, fmt.Errorf("operator client: list request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("operator client: endpoint list is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errorFromResponse(resp, "operator client: endpoint list failed")
	}
	var doc endpointListDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("operator client: endpoint list could not be decoded")
	}
	result := make([]endpointintent.Endpoint, 0, len(doc.Endpoints))
	for _, ed := range doc.Endpoints {
		ep, err := ed.toDomain()
		if err != nil {
			return nil, fmt.Errorf("operator client: endpoint decode failed: %w", err)
		}
		result = append(result, ep)
	}
	return result, nil
}

// Get returns a single endpoint intent by name.
func (c *Client) Get(ctx context.Context, name string) (endpointintent.Endpoint, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return endpointintent.Endpoint{}, fmt.Errorf("operator client: name is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_swobu/endpoints/"+name, nil)
	if err != nil {
		return endpointintent.Endpoint{}, fmt.Errorf("operator client: get request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return endpointintent.Endpoint{}, fmt.Errorf("operator client: endpoint get is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return endpointintent.Endpoint{}, errorFromResponse(resp, "operator client: endpoint get failed")
	}
	var ed endpointDocument
	if err := json.NewDecoder(resp.Body).Decode(&ed); err != nil {
		return endpointintent.Endpoint{}, fmt.Errorf("operator client: endpoint could not be decoded")
	}
	return ed.toDomain()
}

// Put upserts an endpoint intent. The daemon persists the change.
func (c *Client) Put(ctx context.Context, ep endpointintent.Endpoint) (endpointintent.Endpoint, error) {
	ed := endpointDocumentFromDomain(ep)
	raw, err := json.Marshal(ed)
	if err != nil {
		return endpointintent.Endpoint{}, fmt.Errorf("operator client: endpoint save payload could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/_swobu/endpoints/"+ep.Name().String(), bytes.NewReader(raw))
	if err != nil {
		return endpointintent.Endpoint{}, fmt.Errorf("operator client: endpoint save request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return endpointintent.Endpoint{}, fmt.Errorf("operator client: endpoint save is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return endpointintent.Endpoint{}, errorFromResponse(resp, "operator client: endpoint save failed")
	}
	var saved endpointDocument
	if err := json.NewDecoder(resp.Body).Decode(&saved); err != nil {
		return endpointintent.Endpoint{}, fmt.Errorf("operator client: endpoint save response could not be decoded")
	}
	return saved.toDomain()
}

// Delete removes an endpoint intent by name.
func (c *Client) Delete(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("operator client: name is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/_swobu/endpoints/"+name, nil)
	if err != nil {
		return fmt.Errorf("operator client: delete request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("operator client: endpoint delete is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return errorFromResponse(resp, "operator client: endpoint delete failed")
	}
	return nil
}

// CheckClientAccess sends a minimal compatibility probe through one endpoint.
func (c *Client) CheckClientAccess(ctx context.Context, endpointName string, modelID string) (AccessCheckResult, error) {
	endpointName = strings.TrimSpace(endpointName)
	if endpointName == "" {
		return AccessCheckResult{}, fmt.Errorf("operator client: endpoint name is required")
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		modelID = "healthcheck"
	}
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"ping"}],"stream":false}`, modelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/c/"+endpointName+"/chat/completions", strings.NewReader(body))
	if err != nil {
		return AccessCheckResult{}, fmt.Errorf("operator client: client access request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return AccessCheckResult{}, fmt.Errorf("operator client: client access check is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return AccessCheckResult{
			Status:  "reachable",
			Message: fmt.Sprintf("compatibility request succeeded with status %d", resp.StatusCode),
		}, nil
	}
	raw, _ := io.ReadAll(resp.Body)
	message := strings.TrimSpace(string(raw))
	if message == "" {
		message = fmt.Sprintf("compatibility request returned status %d", resp.StatusCode)
	}
	return AccessCheckResult{
		Status:  fmt.Sprintf("backend %d", resp.StatusCode),
		Message: message,
	}, nil
}

// endpointDocument mirrors the HTTP wire format for a single endpoint.
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

type endpointListDocument struct {
	Endpoints []endpointDocument `json:"endpoints"`
}

func endpointDocumentFromDomain(ep endpointintent.Endpoint) endpointDocument {
	providerConfigs := ep.ProviderConfigs()
	doc := endpointDocument{
		Name:                      ep.Name().String(),
		SelectedProviderConfigRef: ep.SelectedProviderConfigRef().String(),
		ProviderConfigs:           make([]providerConfigDocument, 0, len(providerConfigs)),
	}
	for _, pc := range providerConfigs {
		doc.ProviderConfigs = append(doc.ProviderConfigs, providerConfigDocument{
			Ref:           pc.Ref().String(),
			ProviderSpec:  pc.ProviderSpec().String(),
			BaseURL:       pc.BaseURL(),
			CredentialRef: pc.CredentialRef(),
			ModelID:       pc.ModelID(),
			TargetAlias:   pc.TargetAlias(),
			ProtocolKind:  pc.ProtocolKind().String(),
		})
	}
	return doc
}

func (d endpointDocument) toDomain() (endpointintent.Endpoint, error) {
	name, err := endpointintent.ParseEndpointName(d.Name)
	if err != nil {
		return endpointintent.Endpoint{}, err
	}
	selectedRef, err := endpointintent.ParseProviderConfigRef(d.SelectedProviderConfigRef)
	if err != nil {
		return endpointintent.Endpoint{}, err
	}
	providerConfigs := make([]endpointintent.ProviderConfig, 0, len(d.ProviderConfigs))
	for _, pc := range d.ProviderConfigs {
		ref, err := endpointintent.ParseProviderConfigRef(pc.Ref)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		spec, err := endpointintent.ParseProviderSpec(pc.ProviderSpec)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		config, err := endpointintent.NewProviderConfig(ref, spec, pc.BaseURL, pc.CredentialRef, protocolsurface.Kind(pc.ProtocolKind))
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		config, err = config.WithModelID(pc.ModelID)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		config, err = config.WithTargetAlias(pc.TargetAlias)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		providerConfigs = append(providerConfigs, config)
	}
	return endpointintent.NewEndpoint(name, providerConfigs, selectedRef)
}

func errorFromResponse(resp *http.Response, fallback string) error {
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
		if msg := strings.TrimSpace(payload.Error.Message); msg != "" {
			return fmt.Errorf("operator client: %s (code=%s)", msg, payload.Error.Code)
		}
	}
	return fmt.Errorf("%s returned status %d", fallback, resp.StatusCode)
}
