package chatgptlogin

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type captureWriter struct {
	provider string
	key      string
	secret   string
	ref      string
	err      error
}

func (w *captureWriter) Store(providerSpec string, keyName string, secret string) (string, error) {
	w.provider = providerSpec
	w.key = keyName
	w.secret = secret
	if w.err != nil {
		return "", w.err
	}
	if strings.TrimSpace(w.ref) != "" {
		return strings.TrimSpace(w.ref), nil
	}
	return "secret:" + keyName, nil
}

func TestServiceStartCallbackAndSessionSuccess(t *testing.T) {
	t.Parallel()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			t.Fatalf("method=%s", req.Method)
		}
		body, _ := io.ReadAll(req.Body)
		if !strings.Contains(string(body), "grant_type=authorization_code") {
			t.Fatalf("unexpected token body=%s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at_test"}`))
	}))
	defer tokenSrv.Close()

	store := &captureWriter{}
	svc := NewService(http.DefaultClient, ServiceConfig{
		PublicBaseURL:      "http://127.0.0.1:7926",
		AuthorizeURL:       "https://auth.openai.com/oauth/authorize",
		TokenURL:           tokenSrv.URL,
		CallbackListenAddr: "off",
		CredentialOut:      store,
	})

	start, err := svc.Start(context.Background(), StartInput{})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if start.State != SessionPending || start.SessionID == "" {
		t.Fatalf("start=%+v", start)
	}

	u, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	state := strings.TrimSpace(u.Query().Get("state"))
	if state == "" {
		t.Fatal("missing oauth state")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, daemonCallbackPath+"?state="+url.QueryEscape(state)+"&code=code_123", nil)
	svc.HandleCallback(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("callback status=%d", rec.Code)
	}

	status, err := svc.Session(context.Background(), start.SessionID)
	if err != nil {
		t.Fatalf("Session error: %v", err)
	}
	if status.State != SessionSucceeded {
		t.Fatalf("state=%s", status.State)
	}
	if !strings.HasPrefix(status.CredentialRef, "secret:chatgpt/sess_") {
		t.Fatalf("credential ref=%q", status.CredentialRef)
	}
	if store.provider != "chatgpt" {
		t.Fatalf("stored provider=%q", store.provider)
	}
	if !strings.Contains(store.secret, "\"access_token\":\"at_test\"") {
		t.Fatalf("stored credential payload=%q", store.secret)
	}
}

func TestServiceCallbackUnknownState(t *testing.T) {
	t.Parallel()
	svc := NewService(http.DefaultClient, ServiceConfig{CallbackListenAddr: "off"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, daemonCallbackPath+"?state=missing&code=x", nil)
	svc.HandleCallback(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Authentication Error") {
		t.Fatalf("callback body missing auth error title: %q", body)
	}
	if !strings.Contains(body, "help.openai.com") {
		t.Fatalf("callback body missing help center text: %q", body)
	}
}

func TestServiceCallbackOAuthErrorMarksSessionFailed(t *testing.T) {
	t.Parallel()
	svc := NewService(http.DefaultClient, ServiceConfig{CallbackListenAddr: "off"})
	start, err := svc.Start(context.Background(), StartInput{})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	u, _ := url.Parse(start.AuthorizeURL)
	state := u.Query().Get("state")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, daemonCallbackPath+"?state="+url.QueryEscape(state)+"&error=access_denied&request_id=req_123", nil)
	svc.HandleCallback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Authentication Error") {
		t.Fatalf("callback body missing auth error title: %q", body)
	}
	if !strings.Contains(body, "request ID req_123") {
		t.Fatalf("callback body missing request id guidance: %q", body)
	}
	out, err := svc.Session(context.Background(), start.SessionID)
	if err != nil {
		t.Fatalf("Session error: %v", err)
	}
	if out.State != SessionFailed {
		t.Fatalf("state=%s", out.State)
	}
	if !strings.Contains(out.ErrorMessage, "access_denied") {
		t.Fatalf("error=%q", out.ErrorMessage)
	}
	if !strings.Contains(out.ErrorMessage, "req_123") {
		t.Fatalf("error=%q missing request id", out.ErrorMessage)
	}
}

func TestServiceCallbackStateWithAppendedURLStillResolvesSession(t *testing.T) {
	t.Parallel()
	svc := NewService(http.DefaultClient, ServiceConfig{CallbackListenAddr: "off"})
	start, err := svc.Start(context.Background(), StartInput{})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	u, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	state := strings.TrimSpace(u.Query().Get("state"))
	if state == "" {
		t.Fatal("missing oauth state")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		daemonCallbackPath+"?state="+url.QueryEscape(state+"https://github.com/swobuforge/swobu")+"&error=access_denied",
		nil,
	)
	svc.HandleCallback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
	out, err := svc.Session(context.Background(), start.SessionID)
	if err != nil {
		t.Fatalf("Session error: %v", err)
	}
	if out.State != SessionFailed {
		t.Fatalf("state=%s want=%s", out.State, SessionFailed)
	}
	if !strings.Contains(out.ErrorMessage, "access_denied") {
		t.Fatalf("error=%q", out.ErrorMessage)
	}
}

func TestServiceTokenExchangeFailureMarksFailed(t *testing.T) {
	t.Parallel()
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokenSrv.Close()

	svc := NewService(http.DefaultClient, ServiceConfig{TokenURL: tokenSrv.URL, CallbackListenAddr: "off"})
	start, err := svc.Start(context.Background(), StartInput{})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	u, _ := url.Parse(start.AuthorizeURL)
	state := u.Query().Get("state")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, daemonCallbackPath+"?state="+url.QueryEscape(state)+"&code=abc", nil)
	svc.HandleCallback(rec, req)
	out, err := svc.Session(context.Background(), start.SessionID)
	if err != nil {
		t.Fatalf("Session error: %v", err)
	}
	if out.State != SessionFailed {
		t.Fatalf("state=%s", out.State)
	}
	if !strings.Contains(strings.ToLower(out.ErrorMessage), "token exchange") {
		t.Fatalf("error=%q", out.ErrorMessage)
	}
}

func TestServiceCredentialStoreFailureMarksFailed(t *testing.T) {
	t.Parallel()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at_test"}`))
	}))
	defer tokenSrv.Close()

	store := &captureWriter{err: errors.New("keyring write failed for \"chatgpt/sess_x\": backend unavailable")}
	svc := NewService(http.DefaultClient, ServiceConfig{
		TokenURL:           tokenSrv.URL,
		CallbackListenAddr: "off",
		CredentialOut:      store,
	})
	start, err := svc.Start(context.Background(), StartInput{})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	u, _ := url.Parse(start.AuthorizeURL)
	state := u.Query().Get("state")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, daemonCallbackPath+"?state="+url.QueryEscape(state)+"&code=abc", nil)
	svc.HandleCallback(rec, req)
	out, err := svc.Session(context.Background(), start.SessionID)
	if err != nil {
		t.Fatalf("Session error: %v", err)
	}
	if out.State != SessionFailed {
		t.Fatalf("state=%s", out.State)
	}
	if !strings.Contains(strings.ToLower(out.ErrorMessage), "credential store failed: os keyring unavailable") {
		t.Fatalf("error=%q", out.ErrorMessage)
	}
}

func TestServiceSessionRequiresKnownSession(t *testing.T) {
	t.Parallel()
	svc := NewService(nil, ServiceConfig{})
	if _, err := svc.Session(context.Background(), "missing"); err == nil {
		t.Fatal("expected unknown session error")
	}
}

func TestServiceSessionExpires(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	svc := NewService(nil, ServiceConfig{
		CallbackListenAddr: "off",
		Now:                func() time.Time { return now },
	})
	start, err := svc.Start(context.Background(), StartInput{})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	now = now.Add(authSessionTTL + time.Second)
	out, err := svc.Session(context.Background(), start.SessionID)
	if err != nil {
		t.Fatalf("Session error: %v", err)
	}
	if out.State != SessionExpired {
		t.Fatalf("state=%s want=%s", out.State, SessionExpired)
	}
}

func TestServiceStartAuthorizeURLIncludesOriginator(t *testing.T) {
	t.Parallel()
	svc := NewService(nil, ServiceConfig{Originator: "swobu_test", CallbackListenAddr: "off"})
	start, err := svc.Start(context.Background(), StartInput{})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	u, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	if got := strings.TrimSpace(u.Query().Get("originator")); got != "swobu_test" {
		t.Fatalf("originator=%q", got)
	}
}

func TestServiceStartAuthorizeURL_DefaultScopeMatchesCodexContract(t *testing.T) {
	t.Parallel()
	svc := NewService(nil, ServiceConfig{CallbackListenAddr: "off"})
	start, err := svc.Start(context.Background(), StartInput{})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	u, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	if got := strings.TrimSpace(u.Query().Get("scope")); got != "openid profile email offline_access api.connectors.read api.connectors.invoke" {
		t.Fatalf("scope=%q", got)
	}
}

func TestServiceStartAuthorizeURL_DefaultOriginatorMatchesCodex(t *testing.T) {
	t.Parallel()
	svc := NewService(nil, ServiceConfig{CallbackListenAddr: "off"})
	start, err := svc.Start(context.Background(), StartInput{})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	u, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	if got := strings.TrimSpace(u.Query().Get("originator")); got != "codex_cli_rs" {
		t.Fatalf("originator=%q", got)
	}
}

func TestServiceStartAuthorizeURL_MatchesCodexQueryShape(t *testing.T) {
	t.Parallel()
	svc := NewService(nil, ServiceConfig{CallbackListenAddr: "off"})
	start, err := svc.Start(context.Background(), StartInput{})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	u, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	q := u.Query()
	for _, required := range []string{
		"response_type",
		"client_id",
		"redirect_uri",
		"scope",
		"state",
		"code_challenge",
		"code_challenge_method",
		"id_token_add_organizations",
		"codex_cli_simplified_flow",
		"originator",
	} {
		if strings.TrimSpace(q.Get(required)) == "" {
			t.Fatalf("missing required authorize query key=%q in %q", required, start.AuthorizeURL)
		}
	}
	if got := strings.TrimSpace(q.Get("prompt")); got != "" {
		t.Fatalf("unexpected prompt query value=%q", got)
	}
}

func TestServiceStartAuthorizeURL_UsesCodexCallbackPath(t *testing.T) {
	t.Parallel()
	svc := NewService(nil, ServiceConfig{PublicBaseURL: "http://127.0.0.1:7926", CallbackListenAddr: "off"})
	start, err := svc.Start(context.Background(), StartInput{})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	u, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	if got := strings.TrimSpace(u.Query().Get("redirect_uri")); got != "http://localhost:1455"+callbackPath {
		t.Fatalf("redirect_uri=%q", got)
	}
}

func TestServiceTokenExchange_UsesCodexCallbackRedirectURI(t *testing.T) {
	t.Parallel()
	var gotRedirectURI string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		values, _ := url.ParseQuery(string(body))
		gotRedirectURI = strings.TrimSpace(values.Get("redirect_uri"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at_test"}`))
	}))
	defer tokenSrv.Close()

	svc := NewService(http.DefaultClient, ServiceConfig{
		PublicBaseURL:      "http://127.0.0.1:7926",
		TokenURL:           tokenSrv.URL,
		CallbackListenAddr: "off",
		CredentialOut:      &captureWriter{},
	})
	start, err := svc.Start(context.Background(), StartInput{})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	u, _ := url.Parse(start.AuthorizeURL)
	state := strings.TrimSpace(u.Query().Get("state"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, daemonCallbackPath+"?state="+url.QueryEscape(state)+"&code=code_123", nil)
	svc.HandleCallback(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("callback status=%d", rec.Code)
	}
	if _, err := svc.Session(context.Background(), start.SessionID); err != nil {
		t.Fatalf("Session error: %v", err)
	}
	if gotRedirectURI != "http://localhost:1455"+callbackPath {
		t.Fatalf("redirect_uri=%q", gotRedirectURI)
	}
}

func TestServiceSessionSuccess_PlanTierIncludedInCredentialRefWhenPresent(t *testing.T) {
	t.Parallel()

	idToken := testJWTWithPlanType("plus")
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at_test","id_token":"` + idToken + `"}`))
	}))
	defer tokenSrv.Close()

	store := &captureWriter{}
	svc := NewService(http.DefaultClient, ServiceConfig{
		PublicBaseURL:      "http://127.0.0.1:7926",
		AuthorizeURL:       "https://auth.openai.com/oauth/authorize",
		TokenURL:           tokenSrv.URL,
		CallbackListenAddr: "off",
		CredentialOut:      store,
	})

	start, err := svc.Start(context.Background(), StartInput{})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	u, _ := url.Parse(start.AuthorizeURL)
	state := strings.TrimSpace(u.Query().Get("state"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, daemonCallbackPath+"?state="+url.QueryEscape(state)+"&code=code_123", nil)
	svc.HandleCallback(rec, req)

	status, err := svc.Session(context.Background(), start.SessionID)
	if err != nil {
		t.Fatalf("Session error: %v", err)
	}
	if status.State != SessionSucceeded {
		t.Fatalf("state=%s", status.State)
	}
	if !strings.HasPrefix(status.CredentialRef, "secret:chatgpt/plus/sess_") {
		t.Fatalf("credential ref=%q", status.CredentialRef)
	}
}

func testJWTWithPlanType(plan string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_plan_type":"` + plan + `"}}`))
	return header + "." + payload + ".sig"
}

func TestServiceDeviceAuthStartSetsUserAgentHeader(t *testing.T) {
	t.Parallel()
	var seenUA string
	deviceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seenUA = strings.TrimSpace(req.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_auth_id":"dev_123","user_code":"ABCD-1234","interval":"5"}`))
	}))
	defer deviceSrv.Close()

	origDeviceUserCodeURL := deviceUserCodeURL
	origDeviceTokenURL := deviceTokenURL
	origDeviceVerifyURL := deviceVerifyURL
	defer func() {
		deviceUserCodeURL = origDeviceUserCodeURL
		deviceTokenURL = origDeviceTokenURL
		deviceVerifyURL = origDeviceVerifyURL
	}()
	deviceUserCodeURL = deviceSrv.URL
	deviceTokenURL = deviceSrv.URL + "/token"
	deviceVerifyURL = "https://auth.openai.com/codex/device"

	svc := NewService(nil, ServiceConfig{UserAgent: "swobu-test/1"})
	start, err := svc.Start(context.Background(), StartInput{AuthMode: "device"})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if strings.TrimSpace(start.UserCode) == "" {
		t.Fatalf("missing user code in start output: %+v", start)
	}
	if seenUA != "swobu-test/1" {
		t.Fatalf("user-agent=%q", seenUA)
	}
}

func TestServiceDefaults_DoNotEmbedTimeSignedAuthArtifacts(t *testing.T) {
	t.Parallel()
	joined := strings.Join([]string{
		defaultAuthorizeURL,
		defaultTokenURL,
		deviceUserCodeURL,
		deviceTokenURL,
		deviceVerifyURL,
		defaultOpenAIClientID,
		defaultOriginator,
	}, " ")
	forbiddenFragments := []string{
		"payload=",
		"session_id=",
		"verifier_id=",
		"expires=",
		"sig=",
	}
	lower := strings.ToLower(joined)
	for _, fragment := range forbiddenFragments {
		if strings.Contains(lower, fragment) {
			t.Fatalf("defaults contain forbidden signed fragment %q", fragment)
		}
	}
}

func TestServiceStartBrowser_WhenPrimaryCallbackPortBusy_FallsBackToSecondaryPort(t *testing.T) {
	ln1455, err := net.Listen("tcp", "127.0.0.1:1455")
	if err != nil {
		t.Skipf("primary callback port already occupied externally: %v", err)
	}
	defer func() { _ = ln1455.Close() }()

	svc := NewService(nil, ServiceConfig{
		CallbackListenAddr: "127.0.0.1:1455",
	})
	start, err := svc.Start(context.Background(), StartInput{AuthMode: "browser"})
	if err == nil {
		u, parseErr := url.Parse(start.AuthorizeURL)
		if parseErr != nil {
			t.Fatalf("parse authorize url: %v", parseErr)
		}
		if got := strings.TrimSpace(u.Query().Get("redirect_uri")); got != "http://localhost:1457"+callbackPath {
			t.Fatalf("redirect_uri=%q", got)
		}
		return
	}
	t.Fatalf("expected fallback to secondary callback port, got error: %v", err)
}

func TestServiceStartBrowser_WhenCallbackPortsBusy_ReturnsDeviceHint(t *testing.T) {
	ln1455, err := net.Listen("tcp", "127.0.0.1:1455")
	if err != nil {
		t.Skipf("primary callback port already occupied externally: %v", err)
	}
	defer func() { _ = ln1455.Close() }()
	ln1457, err := net.Listen("tcp", "127.0.0.1:1457")
	if err != nil {
		t.Skipf("secondary callback port already occupied externally: %v", err)
	}
	defer func() { _ = ln1457.Close() }()

	svc := NewService(nil, ServiceConfig{
		CallbackListenAddr: "127.0.0.1:1455",
	})
	_, err = svc.Start(context.Background(), StartInput{AuthMode: "browser"})
	if err == nil {
		t.Fatal("expected callback listener unavailable error")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "use device auth mode") {
		t.Fatalf("error missing device hint: %v", err)
	}
}

func TestServiceCallbackServer_ShutsDownAfterTerminalBrowserSession(t *testing.T) {
	now := time.Now().UTC()
	callbackAddr := reserveCallbackAddr(t)
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"at_test"}`))
	}))
	defer tokenSrv.Close()
	svc := NewService(http.DefaultClient, ServiceConfig{
		TokenURL:           tokenSrv.URL,
		CallbackListenAddr: callbackAddr,
		CallbackIdleTTL:    20 * time.Millisecond,
		Now:                func() time.Time { return now },
		CredentialOut:      &captureWriter{},
	})
	start, err := svc.Start(context.Background(), StartInput{AuthMode: "browser"})
	if err != nil {
		t.Fatalf("start browser auth: %v", err)
	}
	u, _ := url.Parse(start.AuthorizeURL)
	state := strings.TrimSpace(u.Query().Get("state"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, daemonCallbackPath+"?state="+url.QueryEscape(state)+"&code=ok", nil)
	svc.HandleCallback(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("callback status=%d", rec.Code)
	}
	if _, err := svc.Session(context.Background(), start.SessionID); err != nil {
		t.Fatalf("session poll: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	waitForCallbackPortRelease(t, callbackAddr, time.Second)
}

func TestServiceCallbackServer_ShutsDownAfterAbandonedBrowserSessionExpiry(t *testing.T) {
	now := time.Now().UTC()
	callbackAddr := reserveCallbackAddr(t)

	svc := NewService(http.DefaultClient, ServiceConfig{
		CallbackListenAddr: callbackAddr,
		CallbackIdleTTL:    15 * time.Millisecond,
		SessionEvictEvery:  10 * time.Millisecond,
		Now:                func() time.Time { return now },
	})

	// Start browser login but never complete callback/poll (abandoned flow).
	started, err := svc.Start(context.Background(), StartInput{AuthMode: "browser"})
	if err != nil {
		t.Fatalf("start browser auth: %v", err)
	}

	// Advance logical time past ttl so eviction loop reclaims stale session.
	now = now.Add(authSessionTTL + 2*time.Second)
	time.Sleep(80 * time.Millisecond)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, err := svc.Session(context.Background(), started.SessionID)
		if err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected session %s to be evicted after expiry", started.SessionID)
}

func waitForCallbackPortRelease(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			_ = ln.Close()
			return
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected callback port %s released, listen failed: %v", addr, lastErr)
}

func reserveCallbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve callback addr: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}
