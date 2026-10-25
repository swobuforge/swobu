package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/operatorweb"
	"github.com/swobuforge/swobu/internal/routing"
)

const browserSessionCookie = "swobu_operator_session"

// BrowserSessions owns ephemeral local host browser authority. Only digests are
// retained; restarting the daemon invalidates every launch token and session.
// Workspace and Node credentials never enter this store.
type BrowserSessions struct {
	mu       sync.Mutex
	launches map[[32]byte]time.Time
	sessions map[[32]byte]time.Time
	now      func() time.Time
}

func NewBrowserSessions() *BrowserSessions {
	return &BrowserSessions{launches: make(map[[32]byte]time.Time), sessions: make(map[[32]byte]time.Time), now: time.Now}
}

func browserSecret() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// IssueLaunch is called only after native local operator admission. Launches
// expire after one minute and are consumed atomically by the session exchange.
func (s *BrowserSessions) IssueLaunch() (string, error) {
	token, err := browserSecret()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	if len(s.launches) >= 64 {
		return "", errLaunchCapacity
	}
	s.launches[sha256.Sum256([]byte(token))] = s.now().Add(time.Minute)
	return token, nil
}

var errLaunchCapacity = errors.New("too many pending browser launches")

func (s *BrowserSessions) prune() {
	now := s.now()
	for key, expiry := range s.launches {
		if !now.Before(expiry) {
			delete(s.launches, key)
		}
	}
	for key, expiry := range s.sessions {
		if !now.Before(expiry) {
			delete(s.sessions, key)
		}
	}
}

func (s *BrowserSessions) exchange(token string) (string, bool) {
	if len(token) != 43 {
		return "", false
	}
	session, err := browserSecret()
	if err != nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	key := sha256.Sum256([]byte(token))
	if _, ok := s.launches[key]; !ok || len(s.sessions) >= 64 {
		return "", false
	}
	delete(s.launches, key)
	s.sessions[sha256.Sum256([]byte(session))] = s.now().Add(12 * time.Hour)
	return session, true
}

func (s *BrowserSessions) authenticated(r *http.Request) bool {
	var value string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == browserSessionCookie {
			count++
			value = cookie.Value
		}
	}
	if count != 1 || len(value) != 43 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	_, ok := s.sessions[sha256.Sum256([]byte(value))]
	return ok
}

func browserRequest(r *http.Request) bool {
	// A session-bearing request must never gain native host authority merely
	// because fetch metadata or User-Agent was omitted by its transport.
	for _, cookie := range r.Cookies() {
		if cookie.Name == browserSessionCookie {
			return true
		}
	}
	return len(r.Header.Values("Origin")) != 0 || r.Header.Get("Sec-Fetch-Site") != "" || r.Header.Get("Sec-Fetch-Mode") != "" || strings.Contains(r.Header.Get("User-Agent"), "Mozilla/")
}

// Protect runs inside local admission and outside the local mux. Browser
// cookies grant control only; they never bypass Workspace data admission.
// Native local transport retains the installation's host authority during A01.
func (s *BrowserSessions) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/browser-launches" {
			if r.Method != http.MethodPost || browserRequest(r) || !IsLocalOperatorRequest(r) || r.URL.RawQuery != "" {
				http.Error(w, "native local operator required", http.StatusForbidden)
				return
			}
			token, err := s.IssueLaunch()
			if err != nil {
				http.Error(w, "browser launch unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"launch_path": "/open#" + token})
			return
		}
		if r.URL.Path == "/api/v1/browser-session" {
			s.serveExchange(w, r)
			return
		}
		if r.URL.Path == "/open" || r.URL.Path == "/operator-bootstrap.js" {
			s.serveBootstrap(w, r)
			return
		}
		control := r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/")
		if control && browserRequest(r) && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && len(r.Header.Values("Origin")) != 1 {
			http.Error(w, "same-origin browser mutation required", http.StatusForbidden)
			return
		}
		// Only real Web Operator documents and assets need a browser session.
		// Unknown paths (including retired routes) fall through to the closed mux
		// and retain 404 semantics without an obsolete namespace exception.
		web := !control && !strings.HasPrefix(r.URL.Path, "/workspaces/") &&
			(operatorweb.IsSPARoute(r.URL.EscapedPath()) || r.URL.Path == "/swobu.png" || strings.HasPrefix(r.URL.Path, "/assets/"))
		// Workspace document routes overlap the data prefix. Navigation requests
		// require browser admission too, without reclassifying protocol requests.
		document := operatorweb.IsSPARoute(r.URL.EscapedPath()) || r.Header.Get("Sec-Fetch-Dest") == "document" || strings.Contains(r.Header.Get("Accept"), "text/html")
		data := strings.HasPrefix(r.URL.Path, "/workspaces/")
		segments := strings.Split(strings.TrimPrefix(r.URL.Path, "/workspaces/"), "/")
		protocol := len(segments) >= 2 && (segments[1] == "v1" || segments[1] == "mcp" || segments[1] == "search" || segments[1] == "responses" || segments[1] == "chat" || segments[1] == "messages" || segments[1] == "models")
		// Browser host authority is control-only. Data requests must arrive with
		// independently admitted Workspace authority; cookies never supply it.
		if data && (protocol || !document) && browserRequest(r) && IsLocalOperatorRequest(r) {
			http.Error(w, "Workspace credential required for data access", http.StatusForbidden)
			return
		}
		if (control && browserRequest(r) || web || document) && !s.authenticated(r) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			http.Error(w, "Run swobu open to open an authenticated Operator session.", http.StatusUnauthorized)
			return
		}
		if control && browserRequest(r) {
			// Recheck session lifetime wherever the existing application repeats
			// ingress authority, including after body decoding and under locks.
			original, _ := r.Context().Value(admissionContextKey{}).(admittedAuthority)
			request := r
			r = r.WithContext(contextWithAuthority(r, func(workspace routing.WorkspaceSlug) (accessidentity.ID, error) {
				if !s.authenticated(request) || original == nil {
					return accessidentity.ID{}, errors.New("browser session expired")
				}
				return original(workspace)
			}))
		}
		next.ServeHTTP(w, r)
	})
}

func (s *BrowserSessions) serveExchange(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost || r.URL.RawQuery != "" || len(r.Header.Values("Origin")) != 1 || ValidateLocalAdmission(r) != nil || !IsLocalOperatorRequest(r) {
		http.Error(w, "same-origin local exchange required", http.StatusForbidden)
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256)
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil {
		http.Error(w, "invalid launch", http.StatusBadRequest)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		http.Error(w, "invalid launch", http.StatusBadRequest)
		return
	}
	session, ok := s.exchange(body.Token)
	if !ok {
		http.Error(w, "launch expired or already used; run swobu open", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: browserSessionCookie, Value: session, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 60 * 60})
	w.WriteHeader(http.StatusNoContent)
}

func (s *BrowserSessions) serveBootstrap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
	if r.Method != http.MethodGet || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/operator-bootstrap.js" {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write([]byte(`const token = location.hash.slice(1); history.replaceState(null, '', '/open'); fetch('/api/v1/browser-session', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({token}), credentials:'same-origin'}).then(response => { if (response.ok) location.replace('/workspaces'); else document.querySelector('p').textContent = 'Launch expired or already used. Run swobu open again.'; }).catch(() => {document.querySelector('p').textContent = 'Cannot reach the Node. Run swobu open again.';});`))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><html lang="en"><meta charset="utf-8"><title>Open Swobu</title><script src="/operator-bootstrap.js" defer></script><p>Opening local Operator session…</p></html>`))
}
