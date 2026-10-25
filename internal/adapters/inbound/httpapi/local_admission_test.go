package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestUnknownBearerCannotAcquireLocalControlAuthority(t *testing.T) {
	calls := 0
	handler := NewLocalAdmissionHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}), nil, nil)
	for _, token := range []string{"node_unrecognized", "arbitrary-bearer"} {
		r := admittedRequest()
		r.URL.Path = "/api/v1/status"
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden || calls != 0 {
			t.Fatalf("unknown bearer acquired local authority: status=%d calls=%d", w.Code, calls)
		}
	}
}

func TestLocalOperatorAdmissionRequiresExactIdentityAndLiveContext(t *testing.T) {
	for name, identity := range map[string]accessidentity.ID{
		"local":     accessidentity.Local,
		"managed":   accessidentity.NewManaged([16]byte{1}),
		"managed-2": accessidentity.NewManaged([16]byte{2}),
		"zero":      {},
	} {
		t.Run(name, func(t *testing.T) {
			r := admittedRequest()
			r = r.WithContext(contextWithAuthority(r, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) { return identity, nil })))
			if got := IsLocalOperatorRequest(r); got != (identity == accessidentity.Local) {
				t.Fatalf("identity=%s operator=%t", name, got)
			}
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			if IsLocalOperatorRequest(r.WithContext(ctx)) {
				t.Fatal("cancelled request retains management admission")
			}
		})
	}
	r := admittedRequest()
	r = r.WithContext(contextWithAuthority(r, admittedAuthority(nil)))
	if _, err := WorkspaceAuthorityFromContext(r.Context(), routing.WorkspaceSlug{}); err == nil {
		t.Fatal("nil closure authorized effect")
	}
	r = admittedRequest()
	r = r.WithContext(contextWithAuthority(r, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) { return accessidentity.ID{}, nil })))
	if _, err := WorkspaceAuthorityFromContext(r.Context(), routing.WorkspaceSlug{}); err == nil {
		t.Fatal("zero identity authorized effect")
	}
	r = admittedRequest()
	r = r.WithContext(contextWithAuthority(r, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) {
		return accessidentity.Local, errors.New("withdrawn")
	})))
	if IsLocalOperatorRequest(r) {
		t.Fatal("withdrawn local identity authorized management")
	}
	r = admittedRequest()
	ctx, cancel := context.WithCancel(r.Context())
	r = r.WithContext(ctx)
	r = r.WithContext(contextWithAuthority(r, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) {
		cancel()
		return accessidentity.Local, nil
	})))
	if IsLocalOperatorRequest(r) {
		t.Fatal("cancellation during authority resolution authorized management")
	}
}

func TestCanonicalControlNamespaceRejectsWorkspaceBearerBeforeAnyLeaf(t *testing.T) {
	store, err := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	_, issued, err := store.Create("consumer", slug)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	handler := NewLocalAdmissionHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusNoContent) }), store, nil)
	for _, path := range []string{"/api/v1", "/api/v1/principals", "/api/v1/access", "/api/v1/workspaces/research/limits", "/api/v1/future-control-leaf", "/api/v1/daemon/shutdown", "/api/v1/telemetry/report"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			req := admittedRequest()
			req.Method = method
			req.URL.Path = path
			req.Header.Set("Authorization", "Bearer "+issued.Token)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != http.StatusForbidden || calls != 0 {
				t.Fatalf("managed Control reached leaf %s %s: %d", method, path, response.Code)
			}
		}
	}
	for _, path := range []string{"/api/v1/principals", "/api/v1/status", "/workspaces/research/v1/models"} {
		req := admittedRequest()
		req.URL.Path = path
		if strings.HasPrefix(path, "/workspaces/") {
			req.Header.Set("Authorization", "Bearer "+issued.Token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusNoContent {
			t.Fatalf("valid local Control/data admission rejected: %s %d", path, response.Code)
		}
	}
}

func TestWorkspaceEffectAuthorityRechecksExactCredentialAndPermission(t *testing.T) {
	store, err := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	principal, issued, err := store.Create("consumer", slug)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := store.IssueCredential(principal.ID, slug)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/workspaces/research/responses", nil)
	request.Header.Set("Authorization", "Bearer "+issued.Token)
	authority, _, err := resolveRequestAuthority(request, store)
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextWithAuthority(request, authority)
	if id, err := WorkspaceAuthorityFromContext(ctx, slug); err != nil || id != principal.ID {
		t.Fatal("initial effect authority denied")
	}
	if _, err := store.RevokeCredential(principal.ID, issued.Credential.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := WorkspaceAuthorityFromContext(ctx, slug); err == nil {
		t.Fatal("revoked credential borrowed sibling authority")
	}
	if _, err := store.Authenticate(sibling.Token); err != nil {
		t.Fatal("sibling unexpectedly revoked")
	}
	request.Header.Set("Authorization", "Bearer "+sibling.Token)
	authority, _, err = resolveRequestAuthority(request, store)
	if err != nil {
		t.Fatal(err)
	}
	ctx = contextWithAuthority(request, authority)
	if err := store.RevokePermission(principal.ID, slug); err != nil {
		t.Fatal(err)
	}
	if _, err := WorkspaceAuthorityFromContext(ctx, slug); err == nil {
		t.Fatal("revoked permission authorized effect")
	}
	if _, err := WorkspaceAuthorityFromContext(context.Background(), slug); err == nil {
		t.Fatal("unadmitted context authorized effect")
	}
}

func admittedRequest() *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9876/workspaces/prod/responses", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	return r
}

func TestAuthorityFromRequestRequiresAdmissionContext(t *testing.T) {
	workspace, _ := routing.ParseWorkspaceSlug("prod")
	if id, err := authorityFromRequest(admittedRequest(), workspace); err == nil || id.Valid() {
		t.Fatalf("authority = %v, %v; missing admission context must fail closed", id, err)
	}
}

func TestManagedAccessCredentialQueryConsumptionPreservesUnrelatedValuesAndErasesRequestURI(t *testing.T) {
	store, _ := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	workspace, _ := routing.ParseWorkspaceSlug("prod")
	_, issued, err := store.Create("CI", workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, rawQuery := range []string{
		"key=" + url.QueryEscape(issued.Token) + "&key=provider-key",
		"key=provider-key&key=" + url.QueryEscape(issued.Token),
		"key=" + url.QueryEscape(" "+issued.Token+" ") + "&key=provider-key",
	} {
		t.Run(rawQuery, func(t *testing.T) {
			r := admittedRequest()
			r.URL.RawQuery = rawQuery
			r.RequestURI = r.URL.RequestURI()
			if _, status, err := resolveRequestAuthority(r, store); err != nil || status != 0 {
				t.Fatalf("resolve = %d, %v", status, err)
			}
			if got := r.URL.Query()["key"]; len(got) != 1 || got[0] != "provider-key" {
				t.Fatalf("preserved key values = %#v", got)
			}
			if strings.Contains(r.URL.RawQuery, issued.Token) || strings.Contains(r.RequestURI, issued.Token) {
				t.Fatalf("secret survived: RawQuery=%q RequestURI=%q", r.URL.RawQuery, r.RequestURI)
			}
		})
	}
}

func TestManagedAccessAuthorizationUsesCaseInsensitiveBearerSchemeOnly(t *testing.T) {
	store, _ := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	workspace, _ := routing.ParseWorkspaceSlug("prod")
	_, issued, _ := store.Create("CI", workspace)

	accepted := admittedRequest()
	accepted.Header.Set("Authorization", "bEaReR "+issued.Token)
	if _, status, err := resolveRequestAuthority(accepted, store); err != nil || status != 0 {
		t.Fatalf("case-insensitive Bearer rejected: %d, %v", status, err)
	}
	if accepted.Header.Get("Authorization") != "" {
		t.Fatal("recognized Authorization credential was not consumed")
	}

	for _, raw := range []string{issued.Token, "Bearer", "Basic " + issued.Token, "Bearer " + issued.Token + " extra"} {
		r := admittedRequest()
		r.Header.Set("Authorization", raw)
		authority, status, err := resolveRequestAuthority(r, store)
		if err != nil || status != 0 {
			t.Fatalf("non-credential Authorization %q affected local admission: %d, %v", raw, status, err)
		}
		if id, err := authority(workspace); err != nil || id != accessidentity.Local {
			t.Fatalf("authority for %q = %v, %v", raw, id, err)
		}
		if got := r.Header.Get("Authorization"); got != raw {
			t.Fatalf("unrecognized Authorization changed: got %q want %q", got, raw)
		}
	}
}

func TestValidateLocalAdmission(t *testing.T) {
	for name, mutate := range map[string]func(*http.Request){"native": func(*http.Request) {}, "matching origin": func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:9876") }} {
		t.Run(name, func(t *testing.T) {
			r := admittedRequest()
			mutate(r)
			if err := ValidateLocalAdmission(r); err != nil {
				t.Fatal(err)
			}
		})
	}
	for name, mutate := range map[string]func(*http.Request){"foreign origin": func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, "null origin": func(r *http.Request) { r.Header.Set("Origin", "null") }, "hostname": func(r *http.Request) { r.Host = "evil.example" }, "foreign peer": func(r *http.Request) { r.RemoteAddr = "192.0.2.1:1234" }} {
		t.Run(name, func(t *testing.T) {
			r := admittedRequest()
			mutate(r)
			if err := ValidateLocalAdmission(r); err == nil {
				t.Fatal("admission unexpectedly succeeded")
			}
		})
	}
}

func TestLocalAdmissionRejectsBrowserOriginAndRebindAttemptsBeforeLeaf(t *testing.T) {
	store, err := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	handler := NewLocalAdmissionHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }), store, nil)
	cases := []struct {
		name   string
		origin string
		host   string
	}{
		{name: "malformed origin", origin: "://", host: "127.0.0.1:9876"},
		{name: "csrf origin", origin: "https://attacker.example", host: "127.0.0.1:9876"},
		{name: "dns rebind host", origin: "http://attacker.example:9876", host: "attacker.example:9876"},
		{name: "origin path", origin: "http://127.0.0.1:9876/cross-site", host: "127.0.0.1:9876"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := admittedRequest()
			request.Host = testCase.host
			request.Header.Set("Origin", testCase.origin)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden || calls != 0 {
				t.Fatalf("status=%d leaf_calls=%d, want forbidden before leaf", response.Code, calls)
			}
		})
	}
}

func TestManagedAccessCredentialSlotsAndConsumption(t *testing.T) {
	store, _ := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	workspace, _ := routing.ParseWorkspaceSlug("prod")
	access, issued, err := store.Create("CI", workspace)
	if err != nil {
		t.Fatal(err)
	}
	slots := map[string]func(*http.Request){"authorization": func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+issued.Token) }, "x-api-key": func(r *http.Request) { r.Header.Set("x-api-key", issued.Token) }, "google": func(r *http.Request) { r.Header.Set("X-Goog-Api-Key", issued.Token) }, "query": func(r *http.Request) {
		q := r.URL.Query()
		q.Set("key", issued.Token)
		q.Set("alt", "sse")
		r.URL.RawQuery = q.Encode()
	}}
	for name, set := range slots {
		t.Run(name, func(t *testing.T) {
			r := admittedRequest()
			set(r)
			authority, status, err := resolveRequestAuthority(r, store)
			if err != nil || status != 0 {
				t.Fatalf("resolve=%d,%v", status, err)
			}
			id, err := authority(workspace)
			if err != nil || id != access.ID {
				t.Fatalf("authority=%v,%v", id, err)
			}
			if r.Header.Get("Authorization") == issued.Token || r.Header.Get("x-api-key") == issued.Token || r.Header.Get("X-Goog-Api-Key") == issued.Token || r.URL.Query().Get("key") != "" {
				t.Fatal("credential was not consumed")
			}
			if name == "query" && r.URL.Query().Get("alt") != "sse" {
				t.Fatal("unrelated query lost")
			}
		})
	}
}

func TestManagedAccessRejectsInvalidAmbiguousAndWrongWorkspace(t *testing.T) {
	store, _ := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
	prod, _ := routing.ParseWorkspaceSlug("prod")
	staging, _ := routing.ParseWorkspaceSlug("staging")
	_, issued, _ := store.Create("CI", prod)
	r := admittedRequest()
	r.Header.Set("Authorization", "Bearer swak_"+url.QueryEscape("bad"))
	if _, status, _ := resolveRequestAuthority(r, store); status != http.StatusUnauthorized {
		t.Fatalf("invalid status=%d", status)
	}
	r = admittedRequest()
	r.Header.Set("Authorization", "Bearer "+issued.Token)
	r.Header.Set("x-api-key", "swak_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if _, status, _ := resolveRequestAuthority(r, store); status != http.StatusUnauthorized {
		t.Fatalf("ambiguous status=%d", status)
	}
	r = admittedRequest()
	r.Header.Set("Authorization", "Bearer "+issued.Token)
	authority, _, _ := resolveRequestAuthority(r, store)
	if _, err := authority(staging); err == nil {
		t.Fatal("wrong Workspace authorized")
	}
	if id, err := authority(prod); err != nil || id == accessidentity.Local {
		t.Fatalf("managed authority=%v,%v", id, err)
	}
}

func TestUnknownCredentialCannotAcquireNativeWorkspaceAuthority(t *testing.T) {
	workspace, _ := routing.ParseWorkspaceSlug("research")
	calls := 0
	handler := NewLocalAdmissionHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if _, err := WorkspaceAuthorityFromContext(r.Context(), workspace); err != nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}), nil, nil)
	for _, path := range []string{"/workspaces/research/v1/models", "/workspaces/research/mcp", "/workspaces/research/search", "/workspaces/research/v1/models"} {
		for _, header := range []string{"Authorization", "x-api-key", "X-Goog-Api-Key"} {
			r := admittedRequest()
			r.URL.Path = path
			value := "node_unrecognized"
			if header == "Authorization" {
				value = "Bearer " + value
			}
			r.Header.Set(header, value)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusUnauthorized || calls != 0 {
				t.Errorf("%s %s: status=%d calls=%d", path, header, w.Code, calls)
			}
		}
	}
}

// Pattern escapes belong only to local Operator documents, never client paths.
func TestLocalAdmissionScopesEscapedModelRouteDocuments(t *testing.T) {
	for _, tc := range []struct {
		path    string
		allowed bool
	}{
		{"/workspaces/dev/services/models/deepseek%2F*", true},
		{"/workspaces/dev/services/models/literal%5C*", true},
		{"/workspaces/dev/services/models/deepseek%2F*/extra", false},
		{"/workspaces/dev/services%2Fmodels/deepseek%2F*", false},
		{"/workspaces/d%65v/services/models/deepseek%2F*", false},
		{"/workspaces/dev/v1/deepseek%2F*", false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
			r := httptest.NewRequest(http.MethodGet, "http://localhost"+tc.path, nil)
			r.RemoteAddr = "127.0.0.1:12345"
			w := httptest.NewRecorder()
			NewLocalAdmissionHandler(next, nil, nil).ServeHTTP(w, r)
			want := http.StatusNotFound
			if tc.allowed {
				want = http.StatusNoContent
			}
			if w.Code != want {
				t.Fatalf("status=%d want=%d", w.Code, want)
			}
		})
	}
}
