// Runtime lane: daemon HTTP client behavior and transport orchestration.
//
// Shared HTTP client for the daemon operator control plane.
// All typed operator clients should use this package
// rather than issuing raw HTTP requests.
package operatorclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

func (c *Client) operatorPath(path string) string {
	return "/api/v1" + path
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/workspaces/"+endpointName+"/chat/completions", strings.NewReader(body))
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

// DaemonVersion reads canonical Node status without legacy-route fallback.
func (c *Client) DaemonVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/status", nil)
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

func (c *Client) GetAuthSessionStatus(ctx context.Context, sessionID string) (AuthSessionStatusResult, error) {
	if !validAuthSessionIdentifier(sessionID) {
		return AuthSessionStatusResult{}, fmt.Errorf("operator client: auth session id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/auth-sessions/"+sessionID, nil)
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
	if doc.SessionID != sessionID || !validAuthSessionState(doc.State) || doc.State == "succeeded" && strings.TrimSpace(doc.CredentialRef) == "" || doc.State != "succeeded" && doc.CredentialRef != "" {
		return AuthSessionStatusResult{}, fmt.Errorf("operator client: invalid auth session status acknowledgement")
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
	if !validAuthSessionIdentifier(sessionID) {
		return fmt.Errorf("operator client: auth session id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/v1/auth-sessions/"+sessionID, nil)
	if err != nil {
		return fmt.Errorf("operator client: auth session cancel request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("operator client: auth session cancel is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errorFromResponse(resp, "operator client: auth session cancel failed")
	}
	var acknowledgement struct {
		SessionID string `json:"session_id"`
		State     string `json:"state"`
	}
	if json.NewDecoder(resp.Body).Decode(&acknowledgement) != nil || acknowledgement.SessionID != sessionID || acknowledgement.State != "canceled" {
		return fmt.Errorf("operator client: auth session cancellation not acknowledged; inspect the original session")
	}
	return nil
}

func errorFromResponse(resp *http.Response, fallback string) error {
	// During the domain cutover, existing command errors retain their owning
	// shapes. Canonical control problems carry detail/title instead of message.
	// Remove retired error shapes with their handler migration, not by guessing
	// at provider prose here.
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Code    string `json:"code"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
		Title   string `json:"title"`
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
		if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/problem+json") {
			message = strings.TrimSpace(payload.Detail) // swobu:io-string source=boundary
			if message == "" {
				message = strings.TrimSpace(payload.Title) // swobu:io-string source=boundary
			}
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

// Session identifiers are opaque single ASCII path segments, never normalized
// locators or URLs. Invalid input must fail before Node contact.
func validAuthSessionIdentifier(id string) bool {
	if id == "" {
		return false
	}
	for _, c := range id {
		if c != '-' && c != '_' && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func validAuthSessionState(state string) bool {
	switch state {
	case "pending", "succeeded", "failed", "expired", "canceled":
		return true
	default:
		return false
	}
}
