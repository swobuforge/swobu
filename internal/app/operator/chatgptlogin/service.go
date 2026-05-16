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
	"log/slog"
	"net"
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
	defaultCallbackListenAddr    = "127.0.0.1:1455"
	fallbackCallbackListenAddr   = "127.0.0.1:1457"
	callbackPath                 = "/auth/callback"
	daemonCallbackPath           = "/_swobu/auth/chatgpt/callback"
	maxHTTPBodyBytes             = 128 * 1024
	authSessionTTL               = 15 * time.Minute
	defaultHTTPTimeout           = 15 * time.Second
	defaultSessionEvictInterval  = 1 * time.Minute
	defaultCallbackIdleTTL       = 3 * time.Minute
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
	Store(providerSpec string, keyName string, secret string) (string, error)
}

type CredentialWriterFunc func(providerSpec string, keyName string, secret string) (string, error)

func (f CredentialWriterFunc) Store(providerSpec string, keyName string, secret string) (string, error) {
	return f(providerSpec, keyName, secret)
}

type ServiceConfig struct {
	PublicBaseURL      string
	AuthorizeURL       string
	TokenURL           string
	ClientID           string
	CallbackListenAddr string
	OAuthRedirectBase  string
	Originator         string
	UserAgent          string
	Now                func() time.Time
	CredentialOut      CredentialWriter
	SessionEvictEvery  time.Duration
	CallbackIdleTTL    time.Duration
}

type Service struct {
	httpClient *http.Client
	config     ServiceConfig

	mu             sync.RWMutex
	sessions       map[string]*sessionRecord
	sessionByState map[string]string

	callbackServer      *http.Server
	callbackListener    net.Listener
	callbackActiveCount int
	callbackIdleTimer   *time.Timer
	callbackIdleTTL     time.Duration
	evictOnce           sync.Once
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
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
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
	if strings.TrimSpace(cfg.AuthorizeURL) == "" { // trimlowerlint:allow boundary canonicalization
		cfg.AuthorizeURL = defaultAuthorizeURL
	}
	if strings.TrimSpace(cfg.TokenURL) == "" { // trimlowerlint:allow boundary canonicalization
		cfg.TokenURL = defaultTokenURL
	}
	if strings.TrimSpace(cfg.ClientID) == "" { // trimlowerlint:allow boundary canonicalization
		cfg.ClientID = defaultOpenAIClientID
	}
	callbackAddr := strings.TrimSpace(cfg.CallbackListenAddr) // trimlowerlint:allow boundary canonicalization
	if strings.EqualFold(callbackAddr, "off") || strings.EqualFold(callbackAddr, "none") {
		cfg.CallbackListenAddr = ""
	} else if callbackAddr == "" {
		cfg.CallbackListenAddr = defaultCallbackListenAddr
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.SessionEvictEvery <= 0 {
		cfg.SessionEvictEvery = defaultSessionEvictInterval
	}
	if cfg.CallbackIdleTTL <= 0 {
		cfg.CallbackIdleTTL = defaultCallbackIdleTTL
	}
	if strings.TrimSpace(cfg.Originator) == "" { // trimlowerlint:allow boundary canonicalization
		cfg.Originator = defaultOriginator
	}
	if strings.TrimSpace(cfg.UserAgent) == "" { // trimlowerlint:allow boundary canonicalization
		cfg.UserAgent = defaultUserAgent
	}
	svc := &Service{
		httpClient:      httpClient,
		config:          cfg,
		sessions:        map[string]*sessionRecord{},
		sessionByState:  map[string]string{},
		callbackIdleTTL: cfg.CallbackIdleTTL,
	}
	svc.startEvictionLoop()
	return svc
}

func (s *Service) startEvictionLoop() {
	s.evictOnce.Do(func() {
		interval := s.config.SessionEvictEvery
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				now := s.config.Now()
				s.mu.Lock()
				s.evictExpiredLocked(now)
				s.mu.Unlock()
			}
		}()
	})
}

func (s *Service) SetPublicBaseURL(raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.PublicBaseURL = canonicalPublicBaseURL(raw)
}

func (s *Service) Start(ctx context.Context, in StartInput) (StartOutput, error) {
	authMode := canonicalAuthMode(in.AuthMode)
	slog.Info("chatgpt auth session start requested",
		"component", "chatgpt_login",
		"auth_mode", authMode,
	)
	if authMode == "browser" {
		s.mu.Lock()
		err := s.ensureCallbackServerStartedLocked()
		s.mu.Unlock()
		if err != nil {
			return StartOutput{}, err
		}
	}
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
	if authMode == "browser" {
		s.callbackActiveCount++
		if s.callbackIdleTimer != nil {
			s.callbackIdleTimer.Stop()
			s.callbackIdleTimer = nil
		}
	}
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
		s.setTerminalStateLocked(rec, SessionExpired, "login session expired")
	}

	if rec.state == SessionPending && rec.deviceAuthID != "" && rec.oauthCode == "" {
		authCode, verifier, done, err := s.pollDeviceToken(ctx, rec.deviceAuthID, rec.deviceUserCode, rec.deviceInterval)
		if err != nil {
			s.setTerminalStateLocked(rec, SessionFailed, err.Error())
		} else if done {
			rec.oauthCode = authCode
			if strings.TrimSpace(verifier) != "" { // trimlowerlint:allow boundary canonicalization
				rec.codeVerifier = strings.TrimSpace(verifier) // trimlowerlint:allow boundary canonicalization
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
			slog.Warn("chatgpt auth session token exchange/persist failed",
				"component", "chatgpt_login",
				"session_id", sessionID,
				"error", err.Error(),
			)
			s.setTerminalStateLocked(rec, SessionFailed, err.Error())
		} else {
			slog.Info("chatgpt auth session succeeded",
				"component", "chatgpt_login",
				"session_id", sessionID,
				"credential_ref", credentialRef,
			)
			rec.credentialRef = credentialRef
			s.setTerminalStateLocked(rec, SessionSucceeded, "")
		}
	}

	out := SessionOutput{
		SessionID:     rec.id,
		State:         rec.state,
		CredentialRef: rec.credentialRef,
		ErrorMessage:  rec.errorMessage,
	}
	s.mu.Unlock()

	if out.State == SessionSucceeded && strings.TrimSpace(out.CredentialRef) == "" { // trimlowerlint:allow boundary canonicalization
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
	state := strings.TrimSpace(q.Get("state")) // trimlowerlint:allow boundary canonicalization
	slog.Info("chatgpt auth callback received",
		"component", "chatgpt_login",
		"has_state", state != "",
		"has_code", strings.TrimSpace(q.Get("code")) != "", // trimlowerlint:allow boundary canonicalization
		"has_error", strings.TrimSpace(q.Get("error")) != "", // trimlowerlint:allow boundary canonicalization
	)
	if state == "" {
		http.Error(w, "missing state", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	sid, ok := lookupSessionIDByCallbackState(s.sessionByState, state)
	if !ok {
		slog.Warn("chatgpt auth callback state did not match active session",
			"component", "chatgpt_login",
		)
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
		slog.Info("chatgpt auth callback ignored because session is terminal",
			"component", "chatgpt_login",
			"session_id", sid,
		)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>Login already completed. You can close this tab.</body></html>"))
		return
	}
	if errValue := strings.TrimSpace(q.Get("error")); errValue != "" { // trimlowerlint:allow boundary canonicalization
		slog.Warn("chatgpt auth callback returned oauth error",
			"component", "chatgpt_login",
			"session_id", sid,
			"oauth_error", errValue,
		)
		message := "oauth error: " + errValue
		if requestID := callbackRequestID(q); requestID != "" {
			message += " (request_id: " + requestID + ")"
		}
		s.setTerminalStateLocked(rec, SessionFailed, message)
		s.mu.Unlock()
		writeAuthenticationErrorPage(w, http.StatusBadRequest, callbackRequestID(q))
		return
	}
	code := strings.TrimSpace(q.Get("code")) // trimlowerlint:allow boundary canonicalization
	if code == "" {
		slog.Warn("chatgpt auth callback missing oauth code",
			"component", "chatgpt_login",
			"session_id", sid,
		)
		message := "oauth callback missing code"
		if requestID := callbackRequestID(q); requestID != "" {
			message += " (request_id: " + requestID + ")"
		}
		s.setTerminalStateLocked(rec, SessionFailed, message)
		s.mu.Unlock()
		writeAuthenticationErrorPage(w, http.StatusBadRequest, callbackRequestID(q))
		return
	}
	rec.oauthCode = code
	rec.state = SessionPending
	slog.Info("chatgpt auth callback accepted oauth code",
		"component", "chatgpt_login",
		"session_id", sid,
	)
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
		if value := strings.TrimSpace(q.Get(key)); value != "" { // trimlowerlint:allow boundary canonicalization
			return value
		}
	}
	return ""
}

func lookupSessionIDByCallbackState(sessionByState map[string]string, rawState string) (string, bool) {
	state := strings.TrimSpace(rawState) // trimlowerlint:allow boundary canonicalization
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
			if sid, ok := sessionByState[strings.TrimSpace(state[:idx])]; ok { // trimlowerlint:allow boundary canonicalization
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
	redirectBase := strings.TrimSpace(s.config.OAuthRedirectBase) // trimlowerlint:allow boundary canonicalization
	if redirectBase == "" {
		redirectBase = "http://localhost:1455"
	}
	redirectURI := strings.TrimRight(redirectBase, "/") + callbackPath
	params := url.Values{}
	params.Set("client_id", strings.TrimSpace(s.config.ClientID)) // trimlowerlint:allow boundary canonicalization
	params.Set("response_type", "code")
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", "openid profile email offline_access api.connectors.read api.connectors.invoke")
	params.Set("state", oauthState)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("id_token_add_organizations", "true")
	params.Set("codex_cli_simplified_flow", "true")
	params.Set("originator", strings.TrimSpace(s.config.Originator))                                         // trimlowerlint:allow boundary canonicalization
	authorizeURL := strings.TrimRight(strings.TrimSpace(s.config.AuthorizeURL), "/") + "?" + params.Encode() // trimlowerlint:allow boundary canonicalization
	if _, err := url.Parse(authorizeURL); err != nil {
		return "", fmt.Errorf("chatgpt authorize url could not be built")
	}
	return authorizeURL, nil
}

func (s *Service) exchangeAndPersist(ctx context.Context, sessionID string, code string, codeVerifier string, redirectOverride string) (string, error) {
	slog.Info("chatgpt auth token exchange started",
		"component", "chatgpt_login",
		"session_id", sessionID,
	)
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", strings.TrimSpace(s.config.ClientID))   // trimlowerlint:allow boundary canonicalization
	form.Set("code", strings.TrimSpace(code))                     // trimlowerlint:allow boundary canonicalization
	redirectBase := strings.TrimSpace(s.config.OAuthRedirectBase) // trimlowerlint:allow boundary canonicalization
	if redirectBase == "" {
		redirectBase = "http://localhost:1455"
	}
	redirectURI := strings.TrimRight(redirectBase, "/") + callbackPath
	if strings.TrimSpace(redirectOverride) != "" { // trimlowerlint:allow boundary canonicalization
		redirectURI = strings.TrimSpace(redirectOverride) // trimlowerlint:allow boundary canonicalization
	}
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", strings.TrimSpace(codeVerifier)) // trimlowerlint:allow boundary canonicalization

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(s.config.TokenURL), strings.NewReader(form.Encode())) // trimlowerlint:allow boundary canonicalization
	if err != nil {
		return "", fmt.Errorf("token exchange request could not be built")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", strings.TrimSpace(s.config.UserAgent)) // trimlowerlint:allow boundary canonicalization
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange failed")
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodyBytes))
	if resp.StatusCode != http.StatusOK {
		slog.Warn("chatgpt auth token exchange failed with non-200 status",
			"component", "chatgpt_login",
			"session_id", sessionID,
			"status_code", resp.StatusCode,
		)
		return "", fmt.Errorf("token exchange returned status %d%s", resp.StatusCode, formatRemoteAuthError(body))
	}
	var token tokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return "", fmt.Errorf("token exchange response could not be decoded")
	}
	access := strings.TrimSpace(token.AccessToken)   // trimlowerlint:allow boundary canonicalization
	refresh := strings.TrimSpace(token.RefreshToken) // trimlowerlint:allow boundary canonicalization
	if access == "" {
		return "", fmt.Errorf("token exchange returned empty access token")
	}
	issuedAt := s.config.Now().UTC()
	expiresAt := time.Time{}
	if token.ExpiresIn > 0 {
		expiresAt = issuedAt.Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	secretPayload := map[string]any{
		"access_token": access,
		"id_token":     strings.TrimSpace(token.IDToken), // trimlowerlint:allow boundary canonicalization
		"issued_at":    issuedAt,
	}
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
			slog.Warn("chatgpt auth credential persistence failed",
				"component", "chatgpt_login",
				"session_id", sessionID,
				"credential_slot", keyName,
				"error", err.Error(),
			)
			return "", fmt.Errorf("%s", classifyCredentialStoreFailure(err))
		}
		if strings.TrimSpace(persistedRef) != "" { // trimlowerlint:allow boundary canonicalization
			credentialRef = strings.TrimSpace(persistedRef) // trimlowerlint:allow boundary canonicalization
		}
	}
	slog.Info("chatgpt auth credential persisted",
		"component", "chatgpt_login",
		"session_id", sessionID,
		"credential_slot", keyName,
	)
	return credentialRef, nil
}

func classifyCredentialStoreFailure(err error) string {
	lower := strings.ToLower(strings.TrimSpace(err.Error())) // trimlowerlint:allow boundary canonicalization
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
	idToken = strings.TrimSpace(idToken) // trimlowerlint:allow boundary canonicalization
	if idToken == "" {
		return "", false
	}
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", false
	}
	payloadPart := parts[1]
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
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
	switch strings.ToLower(strings.TrimSpace(claims.Auth.ChatGPTPlanType)) { // trimlowerlint:allow boundary canonicalization
	case "free":
		return "free", true
	case "plus":
		return "plus", true
	case "pro":
		return "pro", true
	case "team":
		return "team", true
	default:
		return "", false
	}
}

func (s *Service) requestDeviceCode(ctx context.Context) (string, string, time.Duration, error) {
	body, _ := json.Marshal(map[string]string{"client_id": strings.TrimSpace(s.config.ClientID)}) // trimlowerlint:allow boundary canonicalization
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceUserCodeURL, strings.NewReader(string(body)))
	if err != nil {
		return "", "", 0, fmt.Errorf("device auth request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", strings.TrimSpace(s.config.UserAgent)) // trimlowerlint:allow boundary canonicalization
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
	code := strings.TrimSpace(out.UserCode) // trimlowerlint:allow boundary canonicalization
	if code == "" {
		code = strings.TrimSpace(out.UserCodeAlt) // trimlowerlint:allow boundary canonicalization
	}
	if strings.TrimSpace(out.DeviceAuthID) == "" || code == "" { // trimlowerlint:allow boundary canonicalization
		return "", "", 0, fmt.Errorf("device auth start response missing required fields")
	}
	return strings.TrimSpace(out.DeviceAuthID), code, parseDeviceInterval(out.Interval), nil // trimlowerlint:allow boundary canonicalization
}

func (s *Service) pollDeviceToken(ctx context.Context, deviceAuthID string, userCode string, interval time.Duration) (string, string, bool, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	body, _ := json.Marshal(map[string]string{
		"device_auth_id": strings.TrimSpace(deviceAuthID), // trimlowerlint:allow boundary canonicalization
		"user_code":      strings.TrimSpace(userCode),     // trimlowerlint:allow boundary canonicalization
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceTokenURL, strings.NewReader(string(body)))
	if err != nil {
		return "", "", false, fmt.Errorf("device auth poll request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", strings.TrimSpace(s.config.UserAgent)) // trimlowerlint:allow boundary canonicalization
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
	return strings.TrimSpace(out.AuthorizationCode), strings.TrimSpace(out.CodeVerifier), true, nil // trimlowerlint:allow boundary canonicalization
}

func canonicalPublicBaseURL(raw string) string {
	base := strings.TrimSpace(raw) // trimlowerlint:allow boundary canonicalization
	if base == "" {
		base = defaultPublicBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" { // trimlowerlint:allow boundary canonicalization
		return defaultPublicBaseURL
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

func normalizeSessionID(raw string) string {
	id := strings.TrimSpace(raw) // trimlowerlint:allow boundary canonicalization
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
	mode := strings.ToLower(strings.TrimSpace(raw)) // trimlowerlint:allow boundary canonicalization
	if mode == "device" {
		return "device"
	}
	return "browser"
}

func formatRemoteAuthError(raw []byte) string {
	message := strings.TrimSpace(extractRemoteAuthErrorMessage(raw)) // trimlowerlint:allow boundary canonicalization
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
		code := strings.TrimSpace(decoded.Error.Code)       // trimlowerlint:allow boundary canonicalization
		message := strings.TrimSpace(decoded.Error.Message) // trimlowerlint:allow boundary canonicalization
		if code == "" {
			code = strings.TrimSpace(decoded.Code) // trimlowerlint:allow boundary canonicalization
		}
		if message == "" {
			message = strings.TrimSpace(decoded.Message) // trimlowerlint:allow boundary canonicalization
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
	fallback := strings.TrimSpace(string(raw)) // trimlowerlint:allow boundary canonicalization
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
		if n, convErr := strconv.Atoi(strings.TrimSpace(asString)); convErr == nil && n > 0 { // trimlowerlint:allow boundary canonicalization
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
		// Browser-mode sessions hold callback server liveness via
		// callbackActiveCount. Expiry must release that lease even if the
		// operator abandoned login and never polled to terminal state.
		if !rec.terminal && strings.TrimSpace(rec.oauthState) != "" && s.callbackActiveCount > 0 { // trimlowerlint:allow boundary canonicalization
			s.callbackActiveCount--
		}
		delete(s.sessions, sid)
		delete(s.sessionByState, rec.oauthState)
	}
	if s.callbackActiveCount == 0 {
		s.scheduleCallbackShutdownLocked()
	}
}

func (s *Service) ensureCallbackServerStartedLocked() error {
	addr := strings.TrimSpace(s.config.CallbackListenAddr) // trimlowerlint:allow boundary canonicalization
	if addr == "" {
		return nil
	}
	if s.callbackServer != nil && s.callbackListener != nil {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, s.HandleCallback)
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	var (
		ln  net.Listener
		err error
	)
	for _, candidate := range callbackListenCandidates(addr) {
		server.Addr = candidate
		ln, err = net.Listen("tcp", candidate)
		if err == nil {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("chatgpt login callback listener unavailable at %s: %w (use device auth mode if callback ports are in use)", addr, err)
	}
	s.callbackServer = server
	s.callbackListener = ln
	actualPort := ""
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok && tcp.Port > 0 {
		actualPort = fmt.Sprintf("%d", tcp.Port)
	}
	if strings.TrimSpace(s.config.OAuthRedirectBase) == "" && actualPort != "" { // trimlowerlint:allow boundary canonicalization
		s.config.OAuthRedirectBase = "http://localhost:" + actualPort
	}
	go func() {
		_ = server.Serve(ln)
	}()
	return nil
}

func callbackListenCandidates(addr string) []string {
	addr = strings.TrimSpace(addr) // trimlowerlint:allow boundary canonicalization
	if strings.EqualFold(addr, defaultCallbackListenAddr) {
		return []string{defaultCallbackListenAddr, fallbackCallbackListenAddr}
	}
	return []string{addr}
}

func (s *Service) setTerminalStateLocked(rec *sessionRecord, state SessionState, message string) {
	wasTerminal := rec.terminal
	rec.state = state
	rec.errorMessage = message
	rec.terminal = true
	if wasTerminal || strings.TrimSpace(rec.oauthState) == "" { // trimlowerlint:allow boundary canonicalization
		return
	}
	if s.callbackActiveCount > 0 {
		s.callbackActiveCount--
	}
	if s.callbackActiveCount == 0 {
		s.scheduleCallbackShutdownLocked()
	}
}

func (s *Service) scheduleCallbackShutdownLocked() {
	if s.callbackIdleTimer != nil {
		s.callbackIdleTimer.Stop()
	}
	s.callbackIdleTimer = time.AfterFunc(s.callbackIdleTTL, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.callbackActiveCount > 0 || s.callbackServer == nil {
			return
		}
		_ = s.callbackServer.Close()
		s.callbackServer = nil
		s.callbackListener = nil
		s.callbackIdleTimer = nil
	})
}
