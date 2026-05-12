package chatgptlogin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SessionState string

const (
	SessionPending   SessionState = "pending"
	SessionSucceeded SessionState = "succeeded"
	SessionFailed    SessionState = "failed"
	SessionExpired   SessionState = "expired"
)

const (
	defaultPublicBaseURL         = "http://127.0.0.1:7926"
	defaultAuthorizeURL          = "https://auth.openai.com/oauth/authorize"
	defaultTokenURL              = "https://auth.openai.com/oauth/token"
	defaultOpenAIClientID        = "app_EMoamEEZ73f0CkXaXp7hrann"
	daemonCallbackPath           = "/_swobu/auth/chatgpt/callback"
	maxHTTPBodyBytes             = 128 * 1024
	authSessionTTL               = 15 * time.Minute
	defaultHTTPTimeout           = 15 * time.Second
	defaultCredentialKeychainTag = "chatgpt"
	defaultOriginator            = "codex_cli_rs"
	defaultUserAgent             = "swobu/0"
	deviceTokenExchangeRedirect  = "https://auth.openai.com/deviceauth/callback"
)

var (
	deviceUserCodeURL = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	deviceTokenURL    = "https://auth.openai.com/api/accounts/deviceauth/token"
	deviceVerifyURL   = "https://auth.openai.com/codex/device"
)

type CredentialWriter interface {
	Store(providerSpec string, keyName string, secret string) error
}

type CredentialWriterFunc func(providerSpec string, keyName string, secret string) error

func (f CredentialWriterFunc) Store(providerSpec string, keyName string, secret string) error {
	return f(providerSpec, keyName, secret)
}

type ServiceConfig struct {
	PublicBaseURL string
	AuthorizeURL  string
	TokenURL      string
	ClientID      string
	Originator    string
	UserAgent     string
	Now           func() time.Time
	CredentialOut CredentialWriter
}

type Service struct {
	httpClient *http.Client
	config     ServiceConfig

	mu             sync.RWMutex
	sessions       map[string]*sessionRecord
	sessionByState map[string]string
}

type sessionRecord struct {
	id             string
	oauthState     string
	codeVerifier   string
	createdAt      time.Time
	state          SessionState
	oauthCode      string
	deviceAuthID   string
	deviceUserCode string
	deviceInterval time.Duration
	credentialRef  string
	errorMessage   string
	terminal       bool
}

type StartInput struct {
	AuthMode string
}

type StartOutput struct {
	SessionID    string
	AuthorizeURL string
	UserCode     string
	State        SessionState
}

type SessionOutput struct {
	SessionID     string
	State         SessionState
	CredentialRef string
	ErrorMessage  string
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

type deviceUserCodeResponse struct {
	DeviceAuthID string          `json:"device_auth_id"`
	UserCode     string          `json:"user_code"`
	UserCodeAlt  string          `json:"usercode"`
	Interval     json.RawMessage `json:"interval"`
}

type deviceTokenResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
}

func NewService(httpClient *http.Client, cfg ServiceConfig) *Service {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	} else if httpClient.Timeout <= 0 {
		clone := *httpClient
		clone.Timeout = defaultHTTPTimeout
		httpClient = &clone
	}
	cfg.PublicBaseURL = canonicalPublicBaseURL(cfg.PublicBaseURL)
	if strings.TrimSpace(cfg.AuthorizeURL) == "" {
		cfg.AuthorizeURL = defaultAuthorizeURL
	}
	if strings.TrimSpace(cfg.TokenURL) == "" {
		cfg.TokenURL = defaultTokenURL
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		cfg.ClientID = defaultOpenAIClientID
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if strings.TrimSpace(cfg.Originator) == "" {
		cfg.Originator = defaultOriginator
	}
	if strings.TrimSpace(cfg.UserAgent) == "" {
		cfg.UserAgent = defaultUserAgent
	}
	return &Service{
		httpClient:     httpClient,
		config:         cfg,
		sessions:       map[string]*sessionRecord{},
		sessionByState: map[string]string{},
	}
}

func (s *Service) SetPublicBaseURL(raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.PublicBaseURL = canonicalPublicBaseURL(raw)
}

func (s *Service) Start(ctx context.Context, in StartInput) (StartOutput, error) {
	authMode := canonicalAuthMode(in.AuthMode)
	sessionID, err := randomToken(18)
	if err != nil {
		return StartOutput{}, fmt.Errorf("chatgpt login start failed: session id generation failed")
	}
	sessionID = "sess_" + sessionID
	codeVerifier, err := randomToken(48)
	if err != nil {
		return StartOutput{}, fmt.Errorf("chatgpt login start failed: pkce verifier generation failed")
	}
	oauthState := ""
	authURL := ""
	deviceAuthID := ""
	deviceCode := ""
	deviceInterval := 5 * time.Second
	if authMode == "browser" {
		oauthState, err = randomToken(24)
		if err != nil {
			return StartOutput{}, fmt.Errorf("chatgpt login start failed: oauth state generation failed")
		}
		authURL, err = s.buildAuthorizeURL(oauthState, codeVerifier)
		if err != nil {
			return StartOutput{}, fmt.Errorf("chatgpt login start failed: %w", err)
		}
	} else {
		deviceAuthID, deviceCode, deviceInterval, err = s.requestDeviceCode(ctx)
		if err != nil {
			return StartOutput{}, fmt.Errorf("chatgpt login start failed: %w", err)
		}
		authURL = deviceVerifyURL
	}
	now := s.config.Now()
	rec := &sessionRecord{
		id:             sessionID,
		oauthState:     oauthState,
		codeVerifier:   codeVerifier,
		deviceAuthID:   deviceAuthID,
		deviceUserCode: deviceCode,
		deviceInterval: deviceInterval,
		createdAt:      now,
		state:          SessionPending,
	}

	s.mu.Lock()
	s.sessions[sessionID] = rec
	if oauthState != "" {
		s.sessionByState[oauthState] = sessionID
	}
	s.evictExpiredLocked(now)
	s.mu.Unlock()

	return StartOutput{
		SessionID:    sessionID,
		AuthorizeURL: authURL,
		UserCode:     deviceCode,
		State:        SessionPending,
	}, nil
}

func (s *Service) Session(ctx context.Context, sessionID string) (SessionOutput, error) {
	sessionID = normalizeSessionID(sessionID)
	if sessionID == "" {
		return SessionOutput{}, fmt.Errorf("chatgpt login session id is required")
	}

	s.mu.Lock()
	rec, ok := s.sessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return SessionOutput{}, fmt.Errorf("chatgpt login session is unknown")
	}
	now := s.config.Now()
	if now.Sub(rec.createdAt) > authSessionTTL {
		rec.state = SessionExpired
		rec.errorMessage = "login session expired"
		rec.terminal = true
	}

	if rec.state == SessionPending && rec.deviceAuthID != "" && rec.oauthCode == "" {
		authCode, verifier, done, err := s.pollDeviceToken(ctx, rec.deviceAuthID, rec.deviceUserCode, rec.deviceInterval)
		if err != nil {
			rec.state = SessionFailed
			rec.errorMessage = err.Error()
			rec.terminal = true
		} else if done {
			rec.oauthCode = authCode
			if strings.TrimSpace(verifier) != "" {
				rec.codeVerifier = strings.TrimSpace(verifier)
			}
		}
	}

	if rec.state == SessionPending && rec.oauthCode != "" {
		oauthCode := rec.oauthCode
		codeVerifier := rec.codeVerifier
		rec.oauthCode = ""
		s.mu.Unlock()

		redirect := ""
		if rec.deviceAuthID != "" {
			redirect = deviceTokenExchangeRedirect
		}
		credentialRef, err := s.exchangeAndPersist(ctx, sessionID, oauthCode, codeVerifier, redirect)

		s.mu.Lock()
		rec, ok = s.sessions[sessionID]
		if !ok {
			s.mu.Unlock()
			return SessionOutput{}, fmt.Errorf("chatgpt login session is unknown")
		}
		if err != nil {
			rec.state = SessionFailed
			rec.errorMessage = err.Error()
			rec.terminal = true
		} else {
			rec.state = SessionSucceeded
			rec.credentialRef = credentialRef
			rec.errorMessage = ""
			rec.terminal = true
		}
	}

	out := SessionOutput{
		SessionID:     rec.id,
		State:         rec.state,
		CredentialRef: rec.credentialRef,
		ErrorMessage:  rec.errorMessage,
	}
	s.mu.Unlock()

	if out.State == SessionSucceeded && strings.TrimSpace(out.CredentialRef) == "" {
		return SessionOutput{}, fmt.Errorf("chatgpt login succeeded without credential reference")
	}
	return out, nil
}

func (s *Service) HandleCallback(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := req.URL.Query()
	state := strings.TrimSpace(q.Get("state"))
	if state == "" {
		http.Error(w, "missing state", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	sid, ok := lookupSessionIDByCallbackState(s.sessionByState, state)
	if !ok {
		s.mu.Unlock()
		writeAuthenticationErrorPage(w, http.StatusNotFound, callbackRequestID(q))
		return
	}
	rec, ok := s.sessions[sid]
	if !ok {
		s.mu.Unlock()
		writeAuthenticationErrorPage(w, http.StatusNotFound, callbackRequestID(q))
		return
	}
	if rec.terminal {
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>Login already completed. You can close this tab.</body></html>"))
		return
	}
	if errValue := strings.TrimSpace(q.Get("error")); errValue != "" {
		rec.state = SessionFailed
		rec.errorMessage = "oauth error: " + errValue
		if requestID := callbackRequestID(q); requestID != "" {
			rec.errorMessage += " (request_id: " + requestID + ")"
		}
		rec.terminal = true
		s.mu.Unlock()
		writeAuthenticationErrorPage(w, http.StatusBadRequest, callbackRequestID(q))
		return
	}
	code := strings.TrimSpace(q.Get("code"))
	if code == "" {
		rec.state = SessionFailed
		rec.errorMessage = "oauth callback missing code"
		if requestID := callbackRequestID(q); requestID != "" {
			rec.errorMessage += " (request_id: " + requestID + ")"
		}
		rec.terminal = true
		s.mu.Unlock()
		writeAuthenticationErrorPage(w, http.StatusBadRequest, callbackRequestID(q))
		return
	}
	rec.oauthCode = code
	rec.state = SessionPending
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<html><body>ChatGPT login received. You can return to Swobu.</body></html>"))
}

func callbackRequestID(q url.Values) string {
	if q == nil {
		return ""
	}
	candidates := []string{"request_id", "requestId", "x_request_id", "x-request-id"}
	for _, key := range candidates {
		if value := strings.TrimSpace(q.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func lookupSessionIDByCallbackState(sessionByState map[string]string, rawState string) (string, bool) {
	state := strings.TrimSpace(rawState)
	if state == "" {
		return "", false
	}
	if sid, ok := sessionByState[state]; ok {
		return sid, true
	}
	// Some browser/tooling paths accidentally append a URL to the oauth state
	// value during copy/open handoff. Accept the original nonce prefix.
	for _, marker := range []string{"https://", "http://"} {
		if idx := strings.Index(state, marker); idx > 0 {
			if sid, ok := sessionByState[strings.TrimSpace(state[:idx])]; ok {
				return sid, true
			}
		}
	}
	return "", false
}

func writeAuthenticationErrorPage(w http.ResponseWriter, statusCode int, requestID string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)

	message := "An error occurred during authentication. Please try again."
	contact := "You can contact us through our help center at help.openai.com if you keep seeing this error."
	if requestID != "" {
		contact += " (Please include the request ID " + requestID + " in your email.)"
	}
	page := "<html><body>" +
		"<h1>Authentication Error</h1>" +
		"<p>" + html.EscapeString(message) + "</p>" +
		"<p>" + html.EscapeString(contact) + "</p>" +
		"<p>Terms of Use Privacy Policy</p>" +
		"</body></html>"
	_, _ = w.Write([]byte(page))
}

func (s *Service) buildAuthorizeURL(oauthState string, codeVerifier string) (string, error) {
	sum := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	redirectURI := strings.TrimRight(s.config.PublicBaseURL, "/") + daemonCallbackPath
	params := url.Values{}
	params.Set("client_id", strings.TrimSpace(s.config.ClientID))
	params.Set("response_type", "code")
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", "openid profile email offline_access api.connectors.read api.connectors.invoke")
	params.Set("state", oauthState)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("id_token_add_organizations", "true")
	params.Set("codex_cli_simplified_flow", "true")
	params.Set("originator", strings.TrimSpace(s.config.Originator))
	authorizeURL := strings.TrimRight(strings.TrimSpace(s.config.AuthorizeURL), "/") + "?" + params.Encode()
	if _, err := url.Parse(authorizeURL); err != nil {
		return "", fmt.Errorf("chatgpt authorize url could not be built")
	}
	return authorizeURL, nil
}

func (s *Service) exchangeAndPersist(ctx context.Context, sessionID string, code string, codeVerifier string, redirectOverride string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", strings.TrimSpace(s.config.ClientID))
	form.Set("code", strings.TrimSpace(code))
	redirectURI := strings.TrimRight(s.config.PublicBaseURL, "/") + daemonCallbackPath
	if strings.TrimSpace(redirectOverride) != "" {
		redirectURI = strings.TrimSpace(redirectOverride)
	}
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", strings.TrimSpace(codeVerifier))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(s.config.TokenURL), strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("token exchange request could not be built")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", strings.TrimSpace(s.config.UserAgent))
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange failed")
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodyBytes))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange returned status %d%s", resp.StatusCode, formatRemoteAuthError(body))
	}
	var token tokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return "", fmt.Errorf("token exchange response could not be decoded")
	}
	access := strings.TrimSpace(token.AccessToken)
	if access == "" {
		return "", fmt.Errorf("token exchange returned empty access token")
	}

	keyName := defaultCredentialKeychainTag + "/" + sessionID
	if s.config.CredentialOut != nil {
		if err := s.config.CredentialOut.Store("chatgpt", keyName, access); err != nil {
			return "", fmt.Errorf("credential store failed")
		}
	}
	return "keychain:" + keyName, nil
}

func (s *Service) requestDeviceCode(ctx context.Context) (string, string, time.Duration, error) {
	body, _ := json.Marshal(map[string]string{"client_id": strings.TrimSpace(s.config.ClientID)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceUserCodeURL, strings.NewReader(string(body)))
	if err != nil {
		return "", "", 0, fmt.Errorf("device auth request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", strings.TrimSpace(s.config.UserAgent))
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("device auth start failed")
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodyBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", 0, fmt.Errorf("device auth start returned status %d%s", resp.StatusCode, formatRemoteAuthError(raw))
	}
	var out deviceUserCodeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", 0, fmt.Errorf("device auth start response could not be decoded")
	}
	code := strings.TrimSpace(out.UserCode)
	if code == "" {
		code = strings.TrimSpace(out.UserCodeAlt)
	}
	if strings.TrimSpace(out.DeviceAuthID) == "" || code == "" {
		return "", "", 0, fmt.Errorf("device auth start response missing required fields")
	}
	return strings.TrimSpace(out.DeviceAuthID), code, parseDeviceInterval(out.Interval), nil
}

func (s *Service) pollDeviceToken(ctx context.Context, deviceAuthID string, userCode string, interval time.Duration) (string, string, bool, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	body, _ := json.Marshal(map[string]string{
		"device_auth_id": strings.TrimSpace(deviceAuthID),
		"user_code":      strings.TrimSpace(userCode),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceTokenURL, strings.NewReader(string(body)))
	if err != nil {
		return "", "", false, fmt.Errorf("device auth poll request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", strings.TrimSpace(s.config.UserAgent))
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", "", false, fmt.Errorf("device auth poll failed")
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodyBytes))
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return "", "", false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", false, fmt.Errorf("device auth poll returned status %d%s", resp.StatusCode, formatRemoteAuthError(raw))
	}
	var out deviceTokenResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", false, fmt.Errorf("device auth poll response could not be decoded")
	}
	return strings.TrimSpace(out.AuthorizationCode), strings.TrimSpace(out.CodeVerifier), true, nil
}

func canonicalPublicBaseURL(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		base = defaultPublicBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" {
		return defaultPublicBaseURL
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

func normalizeSessionID(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" {
		return ""
	}
	if len(id) > 128 {
		return ""
	}
	for _, r := range id {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if isAlphaNum || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	return id
}

func canonicalAuthMode(raw string) string {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "device" {
		return "device"
	}
	return "browser"
}

func formatRemoteAuthError(raw []byte) string {
	message := strings.TrimSpace(extractRemoteAuthErrorMessage(raw))
	if message == "" {
		return ""
	}
	return ": " + message
}

func extractRemoteAuthErrorMessage(raw []byte) string {
	type envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	var decoded envelope
	if err := json.Unmarshal(raw, &decoded); err == nil {
		code := strings.TrimSpace(decoded.Error.Code)
		message := strings.TrimSpace(decoded.Error.Message)
		if code == "" {
			code = strings.TrimSpace(decoded.Code)
		}
		if message == "" {
			message = strings.TrimSpace(decoded.Message)
		}
		if code != "" && message != "" {
			return fmt.Sprintf("%s (%s)", message, code)
		}
		if message != "" {
			return message
		}
		if code != "" {
			return code
		}
	}
	fallback := strings.TrimSpace(string(raw))
	if fallback == "" {
		return ""
	}
	if len(fallback) > 220 {
		fallback = fallback[:220]
	}
	return fallback
}

func parseDeviceInterval(raw json.RawMessage) time.Duration {
	if len(raw) == 0 {
		return 5 * time.Second
	}
	var asInt int
	if err := json.Unmarshal(raw, &asInt); err == nil && asInt > 0 {
		return time.Duration(asInt) * time.Second
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if n, convErr := strconv.Atoi(strings.TrimSpace(asString)); convErr == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 5 * time.Second
}

func randomToken(numBytes int) (string, error) {
	buf := make([]byte, numBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *Service) evictExpiredLocked(now time.Time) {
	for sid, rec := range s.sessions {
		if now.Sub(rec.createdAt) <= authSessionTTL {
			continue
		}
		delete(s.sessions, sid)
		delete(s.sessionByState, rec.oauthState)
	}
}
