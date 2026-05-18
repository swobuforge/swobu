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
	ExpiresAt    string
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
	UserCode     string
	ExpiresAt    string
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
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
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
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
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
	endpointName = strings.TrimSpace(endpointName) // swobu:io-string source=boundary
	if endpointName == "" {
		return AccessCheckResult{}, fmt.Errorf("operator client: endpoint name is required")
	}
	modelID = strings.TrimSpace(modelID) // swobu:io-string source=boundary
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
	message := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
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
		"provider_spec": strings.TrimSpace(providerSpec), // swobu:io-string source=boundary
		"endpoint_ref":  strings.TrimSpace(endpointRef),  // swobu:io-string source=boundary
		"auth_mode":     strings.TrimSpace(authMode),     // swobu:io-string source=boundary
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
		ExpiresAt    string `json:"expires_at"`
		State        string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return AuthSessionStartResult{}, fmt.Errorf("operator client: auth session start response could not be decoded")
	}
	return AuthSessionStartResult{
		ProviderSpec: strings.TrimSpace(doc.ProviderSpec), // swobu:io-string source=boundary
		SessionID:    strings.TrimSpace(doc.SessionID),    // swobu:io-string source=boundary
		AuthorizeURL: strings.TrimSpace(doc.AuthorizeURL), // swobu:io-string source=boundary
		UserCode:     strings.TrimSpace(doc.UserCode),     // swobu:io-string source=boundary
		ExpiresAt:    strings.TrimSpace(doc.ExpiresAt),    // swobu:io-string source=boundary
		State:        strings.TrimSpace(doc.State),        // swobu:io-string source=boundary
	}, nil
}

func (c *Client) GetAuthSessionStatus(ctx context.Context, sessionID string) (AuthSessionStatusResult, error) {
	sessionID = strings.TrimSpace(sessionID) // swobu:io-string source=boundary
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
		ProviderSpec:  strings.TrimSpace(doc.ProviderSpec),  // swobu:io-string source=boundary
		SessionID:     strings.TrimSpace(doc.SessionID),     // swobu:io-string source=boundary
		State:         strings.TrimSpace(doc.State),         // swobu:io-string source=boundary
		CredentialRef: strings.TrimSpace(doc.CredentialRef), // swobu:io-string source=boundary
		ErrorMessage:  strings.TrimSpace(doc.ErrorMessage),  // swobu:io-string source=boundary
	}, nil
}

func (c *Client) CancelAuthSession(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID) // swobu:io-string source=boundary
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
	sessionID = strings.TrimSpace(sessionID) // swobu:io-string source=boundary
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
		UserCode     string `json:"user_code"`
		ExpiresAt    string `json:"expires_at"`
		State        string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return AuthSessionRetryResult{}, fmt.Errorf("operator client: auth session retry response could not be decoded")
	}
	return AuthSessionRetryResult{
		SessionID:    strings.TrimSpace(doc.SessionID),    // swobu:io-string source=boundary
		AuthorizeURL: strings.TrimSpace(doc.AuthorizeURL), // swobu:io-string source=boundary
		UserCode:     strings.TrimSpace(doc.UserCode),     // swobu:io-string source=boundary
		ExpiresAt:    strings.TrimSpace(doc.ExpiresAt),    // swobu:io-string source=boundary
		State:        strings.TrimSpace(doc.State),        // swobu:io-string source=boundary
	}, nil
}

func errorFromResponse(resp *http.Response, fallback string) error {
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
		if msg := strings.TrimSpace(payload.Error.Message); msg != "" { // swobu:io-string source=boundary
			return fmt.Errorf("operator client: %s (code=%s)", msg, payload.Error.Code)
		}
	}
	return fmt.Errorf("%s returned status %d", fallback, resp.StatusCode)
}
