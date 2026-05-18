package chatgptlogin

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
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
	SessionCanceled  SessionState = "canceled"
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
	if f == nil {
		return "", fmt.Errorf("credential writer function is not configured")
	}
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

type LoginService struct {
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
	ExpiresAt    time.Time
	State        SessionState
}

type SessionOutput struct {
	SessionID     string
	State         SessionState
	CredentialRef string
	ErrorMessage  string
}

func NewService(httpClient *http.Client, cfg ServiceConfig) *LoginService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	} else if httpClient.Timeout <= 0 {
		clone := *httpClient
		clone.Timeout = defaultHTTPTimeout
		httpClient = &clone
	}
	cfg.PublicBaseURL = canonicalPublicBaseURL(cfg.PublicBaseURL)
	if strings.TrimSpace(cfg.AuthorizeURL) == "" { // swobu:io-string source=boundary
		cfg.AuthorizeURL = defaultAuthorizeURL
	}
	if strings.TrimSpace(cfg.TokenURL) == "" { // swobu:io-string source=boundary
		cfg.TokenURL = defaultTokenURL
	}
	if strings.TrimSpace(cfg.ClientID) == "" { // swobu:io-string source=boundary
		cfg.ClientID = defaultOpenAIClientID
	}
	callbackAddr := strings.TrimSpace(cfg.CallbackListenAddr) // swobu:io-string source=boundary
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
	if strings.TrimSpace(cfg.Originator) == "" { // swobu:io-string source=boundary
		cfg.Originator = defaultOriginator
	}
	if strings.TrimSpace(cfg.UserAgent) == "" { // swobu:io-string source=boundary
		cfg.UserAgent = defaultUserAgent
	}
	svc := &LoginService{
		httpClient:      httpClient,
		config:          cfg,
		sessions:        map[string]*sessionRecord{},
		sessionByState:  map[string]string{},
		callbackIdleTTL: cfg.CallbackIdleTTL,
	}
	svc.startEvictionLoop()
	return svc
}

func (s *LoginService) startEvictionLoop() {
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

func (s *LoginService) SetPublicBaseURL(raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.PublicBaseURL = canonicalPublicBaseURL(raw)
}

func (s *LoginService) Start(ctx context.Context, in StartInput) (StartOutput, error) {
	authMode := canonicalAuthMode(in.AuthMode)
	if authMode == "" {
		return StartOutput{}, fmt.Errorf("chatgpt login auth mode is required (browser or device)")
	}
	slog.Info("chatgpt auth session start requested", "component", "chatgpt_login", "auth_mode", authMode)
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
		s.mu.Lock()
		err = s.ensureCallbackServerStartedLocked()
		s.mu.Unlock()
		if err != nil {
			return StartOutput{}, err
		}
		authURL, err = s.buildAuthorizeURL(oauthState, codeVerifier)
		if err != nil {
			s.mu.Lock()
			s.closeCallbackServerIfIdleLocked()
			s.mu.Unlock()
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
		ExpiresAt:    rec.createdAt.Add(authSessionTTL).UTC(),
		State:        SessionPending,
	}, nil
}

// Cancel marks a known auth session as canceled. Browser sessions decrement
// callback listener activity so explicit user cancel can promptly release the
// callback port once no other pending browser sessions remain.
func (s *LoginService) Cancel(sessionID string) error {
	sessionID = normalizeSessionID(sessionID)
	if sessionID == "" {
		return fmt.Errorf("chatgpt login session id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.sessions[sessionID]
	if !ok {
		return fmt.Errorf("chatgpt login session is unknown")
	}
	s.setTerminalStateLocked(rec, SessionCanceled, "login canceled by user")
	return nil
}

func (s *LoginService) Session(ctx context.Context, sessionID string) (SessionOutput, error) {
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
			if strings.TrimSpace(verifier) != "" { // swobu:io-string source=boundary
				rec.codeVerifier = strings.TrimSpace(verifier) // swobu:io-string source=boundary
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
			slog.Warn("chatgpt auth session token exchange/persist failed", "component", "chatgpt_login", "session_id", sessionID, "error", err.Error())
			s.setTerminalStateLocked(rec, SessionFailed, err.Error())
		} else {
			slog.Info("chatgpt auth session succeeded", "component", "chatgpt_login", "session_id", sessionID, "credential_ref", credentialRef)
			rec.credentialRef = credentialRef
			s.setTerminalStateLocked(rec, SessionSucceeded, "")
		}
	}

	out := SessionOutput{SessionID: rec.id, State: rec.state, CredentialRef: rec.credentialRef, ErrorMessage: rec.errorMessage}
	s.mu.Unlock()
	if out.State == SessionSucceeded && strings.TrimSpace(out.CredentialRef) == "" { // swobu:io-string source=boundary
		return SessionOutput{}, fmt.Errorf("chatgpt login succeeded without credential reference")
	}
	return out, nil
}
