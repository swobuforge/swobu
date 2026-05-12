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

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
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

type AuthSessionStartResult struct {
	ProviderSpec string
	SessionID    string
	AuthorizeURL string
	UserCode     string
	State        string
}

type AuthSessionStatusResult struct {
	ProviderSpec  string
	SessionID     string
	State         string
	CredentialRef string
	ErrorMessage  string
}

type AuthSessionRetryResult struct {
	SessionID    string
	AuthorizeURL string
	State        string
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

func (c *Client) StartAuthSession(ctx context.Context, providerSpec string, endpointRef string, authMode string) (AuthSessionStartResult, error) {
	body, err := json.Marshal(map[string]string{
		"provider_spec": strings.TrimSpace(providerSpec),
		"endpoint_ref":  strings.TrimSpace(endpointRef),
		"auth_mode":     strings.TrimSpace(authMode),
	})
	if err != nil {
		return AuthSessionStartResult{}, fmt.Errorf("operator client: auth session payload could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/_swobu/auth/sessions", bytes.NewReader(body))
	if err != nil {
		return AuthSessionStartResult{}, fmt.Errorf("operator client: auth session start request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return AuthSessionStartResult{}, fmt.Errorf("operator client: auth session start is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return AuthSessionStartResult{}, errorFromResponse(resp, "operator client: auth session start failed")
	}
	var doc struct {
		ProviderSpec string `json:"provider_spec"`
		SessionID    string `json:"session_id"`
		AuthorizeURL string `json:"authorize_url"`
		UserCode     string `json:"user_code"`
		State        string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return AuthSessionStartResult{}, fmt.Errorf("operator client: auth session start response could not be decoded")
	}
	return AuthSessionStartResult{
		ProviderSpec: strings.TrimSpace(doc.ProviderSpec),
		SessionID:    strings.TrimSpace(doc.SessionID),
		AuthorizeURL: strings.TrimSpace(doc.AuthorizeURL),
		UserCode:     strings.TrimSpace(doc.UserCode),
		State:        strings.TrimSpace(doc.State),
	}, nil
}

func (c *Client) GetAuthSessionStatus(ctx context.Context, sessionID string) (AuthSessionStatusResult, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return AuthSessionStatusResult{}, fmt.Errorf("operator client: auth session id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_swobu/auth/sessions/"+sessionID, nil)
	if err != nil {
		return AuthSessionStatusResult{}, fmt.Errorf("operator client: auth session status request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return AuthSessionStatusResult{}, fmt.Errorf("operator client: auth session status is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return AuthSessionStatusResult{}, errorFromResponse(resp, "operator client: auth session status failed")
	}
	var doc struct {
		ProviderSpec  string `json:"provider_spec"`
		SessionID     string `json:"session_id"`
		State         string `json:"state"`
		CredentialRef string `json:"credential_ref"`
		ErrorMessage  string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return AuthSessionStatusResult{}, fmt.Errorf("operator client: auth session status response could not be decoded")
	}
	return AuthSessionStatusResult{
		ProviderSpec:  strings.TrimSpace(doc.ProviderSpec),
		SessionID:     strings.TrimSpace(doc.SessionID),
		State:         strings.TrimSpace(doc.State),
		CredentialRef: strings.TrimSpace(doc.CredentialRef),
		ErrorMessage:  strings.TrimSpace(doc.ErrorMessage),
	}, nil
}

func (c *Client) CancelAuthSession(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("operator client: auth session id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/_swobu/auth/sessions/"+sessionID+"/cancel", nil)
	if err != nil {
		return fmt.Errorf("operator client: auth session cancel request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("operator client: auth session cancel is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errorFromResponse(resp, "operator client: auth session cancel failed")
	}
	return nil
}

func (c *Client) RetryAuthSession(ctx context.Context, sessionID string) (AuthSessionRetryResult, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return AuthSessionRetryResult{}, fmt.Errorf("operator client: auth session id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/_swobu/auth/sessions/"+sessionID+"/retry", nil)
	if err != nil {
		return AuthSessionRetryResult{}, fmt.Errorf("operator client: auth session retry request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return AuthSessionRetryResult{}, fmt.Errorf("operator client: auth session retry is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return AuthSessionRetryResult{}, errorFromResponse(resp, "operator client: auth session retry failed")
	}
	var doc struct {
		SessionID    string `json:"session_id"`
		AuthorizeURL string `json:"authorize_url"`
		State        string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return AuthSessionRetryResult{}, fmt.Errorf("operator client: auth session retry response could not be decoded")
	}
	return AuthSessionRetryResult{
		SessionID:    strings.TrimSpace(doc.SessionID),
		AuthorizeURL: strings.TrimSpace(doc.AuthorizeURL),
		State:        strings.TrimSpace(doc.State),
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
