package chatgptlogin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (s *LoginService) buildAuthorizeURL(oauthState string, codeVerifier string) (string, error) {
	sum := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	redirectBase := strings.TrimSpace(s.config.OAuthRedirectBase) // swobu:io-string source=boundary
	if redirectBase == "" {
		redirectBase = "http://localhost:1455"
	}
	redirectURI := strings.TrimRight(redirectBase, "/") + callbackPath
	params := url.Values{}
	params.Set("client_id", strings.TrimSpace(s.config.ClientID)) // swobu:io-string source=boundary
	params.Set("response_type", "code")
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", "openid profile email offline_access api.connectors.read api.connectors.invoke")
	params.Set("state", oauthState)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("id_token_add_organizations", "true")
	params.Set("codex_cli_simplified_flow", "true")
	params.Set("originator", strings.TrimSpace(s.config.Originator))                                         // swobu:io-string source=boundary
	authorizeURL := strings.TrimRight(strings.TrimSpace(s.config.AuthorizeURL), "/") + "?" + params.Encode() // swobu:io-string source=boundary
	if _, err := url.Parse(authorizeURL); err != nil {
		return "", fmt.Errorf("chatgpt authorize url could not be built")
	}
	return authorizeURL, nil
}

func (s *LoginService) exchangeAndPersist(ctx context.Context, sessionID string, code string, codeVerifier string, redirectOverride string) (string, error) {
	slog.Info("chatgpt auth token exchange started", "component", "chatgpt_login", "session_id", sessionID)
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", strings.TrimSpace(s.config.ClientID))   // swobu:io-string source=boundary
	form.Set("code", strings.TrimSpace(code))                     // swobu:io-string source=boundary
	redirectBase := strings.TrimSpace(s.config.OAuthRedirectBase) // swobu:io-string source=boundary
	if redirectBase == "" {
		redirectBase = "http://localhost:1455"
	}
	redirectURI := strings.TrimRight(redirectBase, "/") + callbackPath
	if strings.TrimSpace(redirectOverride) != "" { // swobu:io-string source=boundary
		redirectURI = strings.TrimSpace(redirectOverride) // swobu:io-string source=boundary
	}
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", strings.TrimSpace(codeVerifier)) // swobu:io-string source=boundary

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(s.config.TokenURL), strings.NewReader(form.Encode())) // swobu:io-string source=boundary
	if err != nil {
		return "", fmt.Errorf("token exchange request could not be built")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", strings.TrimSpace(s.config.UserAgent)) // swobu:io-string source=boundary
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange failed")
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodyBytes))
	if resp.StatusCode != http.StatusOK {
		slog.Warn("chatgpt auth token exchange failed with non-200 status", "component", "chatgpt_login", "session_id", sessionID, "status_code", resp.StatusCode)
		return "", fmt.Errorf("token exchange returned status %d%s", resp.StatusCode, formatRemoteAuthError(body))
	}
	var token tokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return "", fmt.Errorf("token exchange response could not be decoded")
	}
	access := strings.TrimSpace(token.AccessToken)   // swobu:io-string source=boundary
	refresh := strings.TrimSpace(token.RefreshToken) // swobu:io-string source=boundary
	if access == "" {
		return "", fmt.Errorf("token exchange returned empty access token")
	}
	issuedAt := s.config.Now().UTC()
	expiresAt := time.Time{}
	if token.ExpiresIn > 0 {
		expiresAt = issuedAt.Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	secretPayload := map[string]any{"access_token": access, "id_token": strings.TrimSpace(token.IDToken), "issued_at": issuedAt} // swobu:io-string source=boundary
	if refresh != "" {
		secretPayload["refresh_token"] = refresh
	}
	if !expiresAt.IsZero() {
		secretPayload["expires_at"] = expiresAt
	}
	encodedSecret, err := json.Marshal(secretPayload)
	if err != nil {
		return "", fmt.Errorf("token exchange response could not be encoded for storage")
	}

	keyName := defaultCredentialKeychainTag + "/" + sessionID
	if tier, ok := parseChatGPTSubscriptionTier(token.IDToken); ok {
		keyName = defaultCredentialKeychainTag + "/" + tier + "/" + sessionID
	}
	credentialRef := "secret:" + keyName
	if s.config.CredentialOut != nil {
		persistedRef, err := s.config.CredentialOut.Store("chatgpt", keyName, string(encodedSecret))
		if err != nil {
			slog.Warn("chatgpt auth credential persistence failed", "component", "chatgpt_login", "session_id", sessionID, "credential_slot", keyName, "error", err.Error())
			return "", fmt.Errorf("%s", classifyCredentialStoreFailure(err))
		}
		if strings.TrimSpace(persistedRef) != "" { // swobu:io-string source=boundary
			credentialRef = strings.TrimSpace(persistedRef) // swobu:io-string source=boundary
		}
	}
	slog.Info("chatgpt auth credential persisted", "component", "chatgpt_login", "session_id", sessionID, "credential_slot", keyName)
	return credentialRef, nil
}

func classifyCredentialStoreFailure(err error) string {
	lower := strings.ToLower(strings.TrimSpace(err.Error())) // swobu:io-string source=boundary
	switch {
	case strings.Contains(lower, "keyring write failed"):
		return "credential store failed: os keyring unavailable"
	case strings.Contains(lower, "permission denied"):
		return "credential store failed: local credential state is not writable"
	default:
		return "credential store failed"
	}
}

func parseChatGPTSubscriptionTier(idToken string) (string, bool) {
	idToken = strings.TrimSpace(idToken) // swobu:io-string source=boundary
	if idToken == "" {
		return "", false
	}
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	var claims struct {
		Auth struct {
			ChatGPTPlanType string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", false
	}
	planType := strings.ToLower(strings.TrimSpace(claims.Auth.ChatGPTPlanType)) // swobu:io-string source=provider-wire
	switch planType {
	case "free", "plus", "pro", "team":
		return planType, true
	default:
		return "", false
	}
}
