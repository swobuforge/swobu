// Runtime lane: daemon HTTP client behavior and transport orchestration.
//
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
	"net/url"
	"strings"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
)

// Client talks to the daemon's semantic operator control plane.
type Client struct {
	http    *http.Client
	baseURL string
}

// New creates a client that talks to the daemon at the given base URL
// (e.g. "http://127.0.0.1:9876").
func New(httpClient *http.Client, baseURL string) *Client {
	return &Client{http: httpClient, baseURL: strings.TrimRight(baseURL, "/")}
}

// CheckClientAccess sends a minimal client-access probe through one endpoint.
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
			Message: fmt.Sprintf("client-access request succeeded with status %d", resp.StatusCode),
		}, nil
	}
	raw, _ := io.ReadAll(resp.Body)
	message := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if message == "" {
		message = fmt.Sprintf("client-access request returned status %d", resp.StatusCode)
	}
	return AccessCheckResult{
		Status:  fmt.Sprintf("backend %d", resp.StatusCode),
		Message: message,
	}, nil
}

// Status returns the daemon-owned traffic projection for an operator scope.
func (c *Client) Status(ctx context.Context, scope string) (StatusProjection, error) {
	scope = strings.TrimSpace(scope) // swobu:io-string source=boundary
	if scope == "" {
		return StatusProjection{}, fmt.Errorf("operator client: status projection scope is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_swobu/status-projection?scope="+url.QueryEscape(scope), nil)
	if err != nil {
		return StatusProjection{}, fmt.Errorf("operator client: status projection request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return StatusProjection{}, fmt.Errorf("operator client: status projection is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return StatusProjection{}, errorFromResponse(resp, "operator client: status projection failed")
	}
	var projection StatusProjection
	if err := json.NewDecoder(resp.Body).Decode(&projection); err != nil {
		return StatusProjection{}, fmt.Errorf("operator client: status projection could not be decoded")
	}
	return projection, nil
}

// DaemonVersion returns the daemon's version from the /_swobu/status endpoint.
func (c *Client) DaemonVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_swobu/status", nil)
	if err != nil {
		return "", fmt.Errorf("operator client: status request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("operator client: status is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", errorFromResponse(resp, "operator client: status failed")
	}
	var payload struct {
		SwobuVersion string `json:"swobu_version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("operator client: status could not be decoded")
	}
	return strings.TrimSpace(payload.SwobuVersion), nil
}

func (c *Client) StartAuthSession(ctx context.Context, providerSpec string, workspace string, route string, targetID string, draftSubject string, authMode string) (AuthSessionStartResult, error) {
	body, err := json.Marshal(map[string]string{
		"provider_spec": strings.TrimSpace(providerSpec), // swobu:io-string source=boundary
		"workspace":     strings.TrimSpace(workspace), "route": strings.TrimSpace(route), "target_id": strings.TrimSpace(targetID), "draft_subject": strings.TrimSpace(draftSubject),
		"auth_mode": strings.TrimSpace(authMode), // swobu:io-string source=boundary
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

func (c *Client) ProbeTarget(ctx context.Context, connection workspaceapi.Connection, providerProtocol string) (ModelCatalogResult, error) {
	body, err := json.Marshal(struct {
		Connection       workspaceapi.Connection `json:"connection"`
		ProviderProtocol string                  `json:"provider_protocol,omitempty"`
	}{Connection: connection, ProviderProtocol: strings.TrimSpace(providerProtocol)})
	if err != nil {
		return ModelCatalogResult{}, fmt.Errorf("operator client: model catalog payload could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/_swobu/target-probe", bytes.NewReader(body))
	if err != nil {
		return ModelCatalogResult{}, fmt.Errorf("operator client: model catalog request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return ModelCatalogResult{}, fmt.Errorf("operator client: model catalog is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ModelCatalogResult{}, errorFromResponse(resp, "operator client: model catalog probe failed")
	}
	var result ModelCatalogResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ModelCatalogResult{}, fmt.Errorf("operator client: model catalog response could not be decoded")
	}
	return result, nil
}

func errorFromResponse(resp *http.Response, fallback string) error {
	// The daemon emits two canonical error shapes at this boundary. Workspace
	// and credential commands marshal the typed workspaces.CommandError flat
	// ({code,message}); the ChatGPT login flow normalizes remote OAuth errors
	// nested ({error:{code,message}}). Decode both so every daemon command
	// surfaces its real cause instead of collapsing to a status-code fallback.
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
		code := strings.TrimSpace(payload.Error.Code)       // swobu:io-string source=boundary
		message := strings.TrimSpace(payload.Error.Message) // swobu:io-string source=boundary
		if code == "" {
			code = strings.TrimSpace(payload.Code) // swobu:io-string source=boundary
		}
		if message == "" {
			message = strings.TrimSpace(payload.Message) // swobu:io-string source=boundary
		}
		if message != "" {
			return &ResponseError{
				StatusCode: resp.StatusCode,
				Code:       code,
				Message:    message,
				Fallback:   fallback,
			}
		}
	}
	return &ResponseError{
		StatusCode: resp.StatusCode,
		Fallback:   fallback,
	}
}
