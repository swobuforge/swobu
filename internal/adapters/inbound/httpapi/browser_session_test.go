package httpapi

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/routing"
)

func sessionRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, "http://127.0.0.1:9876"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:12345"
	return r
}

func TestBrowserSessionExchangeReplayExpiryAndCookie(t *testing.T) {
	sessions := NewBrowserSessions()
	now := time.Now()
	sessions.now = func() time.Time { return now }
	handler := NewLocalAdmissionHandler(sessions.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })), nil, nil)
	token, err := sessions.IssueLaunch()
	if err != nil {
		t.Fatal(err)
	}
	exchange := func(token, origin string) *httptest.ResponseRecorder {
		r := sessionRequest("POST", "/api/v1/browser-session", fmt.Sprintf(`{"token":%q}`, token))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	foreign := exchange(token, "https://attacker.example")
	if foreign.Code != 403 {
		t.Fatal(foreign.Code)
	}
	w := exchange(token, "http://127.0.0.1:9876")
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing cookie")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure || cookie.Path != "/" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(cookie)
	}
	if exchange(token, "http://127.0.0.1:9876").Code != 401 {
		t.Fatal("replayed launch")
	}
	for _, path := range []string{"/api/v1/status", "/workspaces"} {
		r := sessionRequest("GET", path, "")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		denied := httptest.NewRecorder()
		handler.ServeHTTP(denied, r)
		if denied.Code != 401 {
			t.Fatal(path, denied.Code)
		}
		r.AddCookie(cookie)
		allowed := httptest.NewRecorder()
		handler.ServeHTTP(allowed, r)
		if allowed.Code != 204 {
			t.Fatal(path, allowed.Code)
		}
		r.AddCookie(cookie)
		duplicate := httptest.NewRecorder()
		handler.ServeHTTP(duplicate, r)
		if duplicate.Code != 401 {
			t.Fatal("duplicate cookie admitted")
		}
	}
	pending, _ := sessions.IssueLaunch()
	now = now.Add(time.Minute)
	if exchange(pending, "http://127.0.0.1:9876").Code != 401 {
		t.Fatal("expired launch admitted")
	}
	now = now.Add(12 * time.Hour)
	r := sessionRequest("GET", "/api/v1/status", "")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.AddCookie(cookie)
	expired := httptest.NewRecorder()
	handler.ServeHTTP(expired, r)
	if expired.Code != 401 {
		t.Fatal("expired session admitted")
	}
}

func TestBrowserLaunchNativeOnlyAndBootstrapPolicy(t *testing.T) {
	sessions := NewBrowserSessions()
	handler := NewLocalAdmissionHandler(sessions.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })), nil, nil)
	for _, browser := range []bool{false, true} {
		r := sessionRequest("POST", "/api/v1/browser-launches", "")
		if browser {
			r.Header.Set("Sec-Fetch-Site", "same-origin")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		want := 201
		if browser {
			want = 403
		}
		if w.Code != want {
			t.Fatal(w.Code)
		}
		if !browser && (!strings.Contains(w.Body.String(), "/open#") || w.Header().Get("Cache-Control") != "no-store") {
			t.Fatal("launch is not fragment-only/no-store")
		}
	}
	for _, path := range []string{"/open", "/operator-bootstrap.js"} {
		r := sessionRequest("GET", path, "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("Referrer-Policy") != "no-referrer" || strings.Contains(w.Header().Get("Content-Security-Policy"), "unsafe-inline") {
			t.Fatal(w.Code, w.Header())
		}
	}
	token, _ := sessions.IssueLaunch()
	r := sessionRequest("POST", "/api/v1/browser-session", fmt.Sprintf(`{"token":%q}`, token))
	r.TLS = &tls.ConnectionState{}
	r.Header.Set("Origin", "https://127.0.0.1:9876")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 204 || !w.Result().Cookies()[0].Secure {
		t.Fatal("TLS session lacks Secure")
	}
}

func TestBrowserLaunchIsConsumedOnceConcurrently(t *testing.T) {
	sessions := NewBrowserSessions()
	token, _ := sessions.IssueLaunch()
	var group sync.WaitGroup
	successes := make(chan bool, 16)
	for range 16 {
		group.Add(1)
		go func() { defer group.Done(); _, ok := sessions.exchange(token); successes <- ok }()
	}
	group.Wait()
	close(successes)
	count := 0
	for ok := range successes {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("launch consumed %d times", count)
	}
}

func TestBrowserSessionCannotPromoteWorkspaceCredential(t *testing.T) {
	store, err := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := routing.ParseWorkspaceSlug("research")
	_, issued, err := store.Create("client", workspace)
	if err != nil {
		t.Fatal(err)
	}
	sessions := NewBrowserSessions()
	token, _ := sessions.IssueLaunch()
	session, _ := sessions.exchange(token)
	calls := 0
	handler := NewLocalAdmissionHandler(sessions.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) })), store, nil)
	for _, path := range []string{"/api/v1/status", "/api/v1/browser-launches", "/api/v1/browser-session"} {
		r := sessionRequest("POST", path, `{}`)
		r.Header.Set("Authorization", "Bearer "+issued.Token)
		r.Header.Set("Origin", "http://127.0.0.1:9876")
		r.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: session})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 403 || calls != 0 {
			t.Fatalf("Workspace credential promoted at %s: %d", path, w.Code)
		}
	}
}

func TestBrowserSessionAuthorityExpiresBeforeEffect(t *testing.T) {
	sessions := NewBrowserSessions()
	now := time.Now()
	sessions.now = func() time.Time { return now }
	token, _ := sessions.IssueLaunch()
	session, _ := sessions.exchange(token)
	handler := NewLocalAdmissionHandler(sessions.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsLocalOperatorRequest(r) {
			t.Fatal("fresh session not admitted")
		}
		now = now.Add(12 * time.Hour)
		if IsLocalOperatorRequest(r) {
			t.Fatal("expired session retains effect authority")
		}
		w.WriteHeader(204)
	})), nil, nil)
	r := sessionRequest("GET", "/api/v1/status", "")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: session})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
}

func TestBrowserSessionRejectsMalformedExchangeAndOriginlessMutation(t *testing.T) {
	sessions := NewBrowserSessions()
	token, _ := sessions.IssueLaunch()
	handler := NewLocalAdmissionHandler(sessions.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })), nil, nil)
	for _, body := range []string{`{`, `{"token":"` + token + `","extra":true}`, `{"token":"` + token + `"} {}`, strings.Repeat("x", 257)} {
		r := sessionRequest("POST", "/api/v1/browser-session", body)
		r.Header.Set("Origin", "http://127.0.0.1:9876")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("malformed exchange returned %d", w.Code)
		}
	}
	session, ok := sessions.exchange(token)
	if !ok {
		t.Fatal("malformed exchange consumed launch")
	}
	r := sessionRequest("POST", "/api/v1/workspaces", `{}`)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: session})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("browser mutation without Origin admitted")
	}
}

func TestBrowserSessionBoundsAndRestartInvalidation(t *testing.T) {
	sessions := NewBrowserSessions()
	now := time.Now()
	sessions.now = func() time.Time { return now }
	var tokens []string
	for range 64 {
		token, err := sessions.IssueLaunch()
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}
	if _, err := sessions.IssueLaunch(); err == nil {
		t.Fatal("unbounded pending launches")
	}
	var firstSession string
	for _, token := range tokens {
		session, ok := sessions.exchange(token)
		if !ok {
			t.Fatal("valid launch failed")
		}
		if firstSession == "" {
			firstSession = session
		}
	}
	token, err := sessions.IssueLaunch()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sessions.exchange(token); ok {
		t.Fatal("unbounded browser sessions")
	}
	r := sessionRequest("GET", "/api/v1/status", "")
	r.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: firstSession})
	if NewBrowserSessions().authenticated(r) {
		t.Fatal("session survived daemon restart")
	}
	if _, ok := NewBrowserSessions().exchange(token); ok {
		t.Fatal("launch survived daemon restart")
	}
	now = now.Add(12 * time.Hour)
	fresh, err := sessions.IssueLaunch()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sessions.exchange(fresh); !ok {
		t.Fatal("expired sessions did not release capacity")
	}
}

func TestSessionCookieCannotFallBackToNativeAuthority(t *testing.T) {
	sessions := NewBrowserSessions()
	calls := 0
	handler := NewLocalAdmissionHandler(sessions.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	})), nil, nil)
	// Clients and proxies may omit fetch metadata. A presented session cookie
	// must still select browser admission, even when expired or unrecognized.
	r := sessionRequest(http.MethodGet, "/api/v1/status", "")
	r.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: strings.Repeat("x", 43)})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || calls != 0 {
		t.Fatalf("session cookie fell back to native authority: status=%d calls=%d", w.Code, calls)
	}
}

func TestBrowserControlSessionCannotAuthorizeWorkspaceData(t *testing.T) {
	sessions := NewBrowserSessions()
	token, _ := sessions.IssueLaunch()
	session, _ := sessions.exchange(token)
	calls := 0
	handler := NewLocalAdmissionHandler(sessions.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	})), nil, nil)
	for _, path := range []string{"/workspaces/research/v1/models", "/workspaces/research/mcp", "/workspaces/research/search"} {
		r := sessionRequest(http.MethodGet, path, "")
		r.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: session})
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden || calls != 0 {
			t.Fatalf("browser control session authorized Workspace data at %s: status=%d calls=%d", path, w.Code, calls)
		}
	}
}

func TestBrowserCookieDoesNotReplaceValidWorkspaceBearer(t *testing.T) {
	store, err := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := routing.ParseWorkspaceSlug("research")
	_, issued, err := store.Create("client", workspace)
	if err != nil {
		t.Fatal(err)
	}
	sessions := NewBrowserSessions()
	called := false
	handler := NewLocalAdmissionHandler(sessions.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := WorkspaceAuthorityFromContext(r.Context(), workspace)
		if err != nil || !identity.Managed() {
			t.Fatal("lost independently admitted Workspace authority")
		}
		called = true
		w.WriteHeader(http.StatusNoContent)
	})), store, nil)
	r := sessionRequest(http.MethodGet, "/workspaces/research/v1/models", "")
	r.Header.Set("Authorization", "Bearer "+issued.Token)
	r.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: strings.Repeat("x", 43)})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || !called {
		t.Fatalf("valid bearer denied: %d", w.Code)
	}
}

// An unknown local path must reach the closed ServeMux instead of turning into
// a browser-login challenge. This preserves exact 404 behavior when an old
// operation is deleted without introducing an alias or special legacy matcher.
func TestBrowserProtectUnknownPathsFallThroughToNotFound(t *testing.T) {
	sessions := NewBrowserSessions()
	handler := sessions.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	for _, path := range []string{"/_swobu/down", "/_swobu/telemetry-report", "/_swobu/credential-files", "/unrecognized"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, sessionRequest(method, path, `{}`))
			if w.Code != http.StatusNotFound {
				t.Fatalf("unknown %s %s returned %d, want 404", method, path, w.Code)
			}
		}
	}
	// Valid Web document paths still require browser admission.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, sessionRequest(http.MethodGet, "/workspaces/example/services/models", ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Web document unexpectedly bypassed session: %d", w.Code)
	}
}
