package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	connectionapp "github.com/swobuforge/swobu/internal/app/operator/connections"
	"github.com/swobuforge/swobu/internal/configstore"
	domain "github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/connectionfixture"
)

func TestConnectionControlReadsAdmissionRevisionAndRedaction(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := connectionfixture.Open(filepath.Join(dir, "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := configstore.OpenOrCreate(filepath.Join(dir, "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer workspaces.Close()
	id, _ := connectionidentity.New()
	endpoint, _ := domain.ParseEndpoint("https://example.com/mcp")
	spec, _ := domain.NewMCPHTTPSpec(endpoint, credentialref.Parse("env:PRIVATE_LOCATOR"))
	value, err := domain.New(id, "Shared", spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(value, domain.Revision{}); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := domain.NewLifecycle(registry, workspaces)
	if err != nil {
		t.Fatal(err)
	}
	queries, err := connectionapp.NewQueries(registry, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewConnectionControlHandler(queries, nil, nil, nil)
	for _, test := range []struct {
		method, path string
		trusted      bool
		status       int
		etag         string
	}{
		{"GET", "/api/v1/connections", false, 403, ""},
		{"GET", "/api/v1/connections", true, 200, ""},
		{"GET", "/api/v1/connections?kind=mcp&q=shared", true, 200, ""},
		{"GET", "/api/v1/connections?kind=unknown", true, 400, ""},
		{"GET", "/api/v1/connections?q=one&q=two", true, 400, ""},
		{"GET", "/api/v1/connections?unexpected=secret", true, 400, ""},
		{"GET", "/api/v1/connections/" + id.String(), true, 200, `"1"`},
		{"GET", "/api/v1/connections/" + id.String() + "/references", true, 200, ""},
		{"GET", "/api/v1/connections/" + id.String() + "?token=secret", true, 400, ""},
		{"GET", "/api/v1/connections/Invalid", true, 400, ""},
		{"GET", "/api/v1/connections/missing", true, 404, ""},
		{"GET", "/api/v1/connections/" + id.String() + "/unknown", true, 404, ""},
		{"POST", "/api/v1/connections", true, 400, ""},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			r := httptest.NewRequest(test.method, test.path, nil)
			if test.trusted {
				r = r.WithContext(contextWithAuthority(r, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) { return accessidentity.Local, nil })))
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != test.status || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != test.etag {
				t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
			}
			if strings.Contains(w.Body.String(), "PRIVATE_LOCATOR") || strings.Contains(w.Body.String(), "token=secret") {
				t.Fatal("response exposed sensitive input")
			}
			if test.status >= 400 {
				var problem ControlProblem
				if w.Header().Get("Content-Type") != "application/problem+json" || json.Unmarshal(w.Body.Bytes(), &problem) != nil || problem.Status != test.status || problem.Code == "" {
					t.Fatal("noncanonical error")
				}
			}
		})
	}
}

func TestConnectionControlRejectsWorkspaceAudienceAndUnavailableQueries(t *testing.T) {
	for _, identity := range []accessidentity.ID{accessidentity.NewManaged([16]byte{1}), accessidentity.Local} {
		r := httptest.NewRequest("GET", "/api/v1/connections", nil)
		r = r.WithContext(contextWithAuthority(r, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) { return identity, nil })))
		w := httptest.NewRecorder()
		NewConnectionControlHandler(nil, nil, nil, nil).ServeHTTP(w, r)
		want := http.StatusForbidden
		if identity == accessidentity.Local {
			want = http.StatusServiceUnavailable
		}
		if w.Code != want {
			t.Fatalf("status=%d want=%d", w.Code, want)
		}
	}
}

func TestConnectionDeletePreconditionsBlockersAndAudit(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := connectionfixture.Open(filepath.Join(dir, "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := configstore.OpenOrCreate(filepath.Join(dir, "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer workspaces.Close()
	endpoint, _ := domain.ParseEndpoint("https://example.com/mcp")
	spec, _ := domain.NewMCPHTTPSpec(endpoint, credentialref.Parse("env:PRIVATE_LOCATOR"))
	id, _ := connectionidentity.New()
	value, err := domain.New(id, "Shared", spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(value, domain.Revision{}); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := domain.NewLifecycle(registry, workspaces)
	if err != nil {
		t.Fatal(err)
	}
	queries, _ := connectionapp.NewQueries(registry, lifecycle)
	commands, _ := connectionapp.NewCommands(registry, lifecycle)
	handler := NewConnectionControlHandler(queries, commands, nil, nil)
	path := "/api/v1/connections/" + id.String()
	withdrawAtPublication := false
	request := func(revisions []string, identity accessidentity.ID) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodDelete, path, nil)
		for _, revision := range revisions {
			r.Header.Add("If-Match", revision)
		}
		calls := 0
		r = r.WithContext(contextWithAuthority(r, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) {
			calls++
			if withdrawAtPublication && calls > 1 {
				return accessidentity.ID{}, errors.New("withdrawn")
			}
			return identity, nil
		})))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "PRIVATE_LOCATOR") {
			t.Fatal("cacheable or sensitive response")
		}
		return w
	}
	for _, test := range []struct {
		tags   []string
		status int
	}{
		{nil, 428}, {[]string{`W/"1"`}, 400}, {[]string{`"1"`, `"1"`}, 400},
		{[]string{`"01"`}, 400}, {[]string{`"2"`}, 412},
	} {
		w := request(test.tags, accessidentity.Local)
		if w.Code != test.status {
			t.Fatalf("tags=%v status=%d body=%s", test.tags, w.Code, w.Body)
		}
		if w.Code == 412 && (!strings.Contains(w.Body.String(), `"current":`) || !strings.Contains(w.Body.String(), `"revision":"1"`)) {
			t.Fatal("missing reload summary")
		}
	}
	if w := request([]string{`"1"`}, accessidentity.NewManaged([16]byte{1})); w.Code != 403 {
		t.Fatal(w.Code)
	}
	withdrawAtPublication = true
	if w := request([]string{`"1"`}, accessidentity.Local); w.Code != 403 {
		t.Fatalf("withdrawn authority: %d %s", w.Code, w.Body)
	}
	withdrawAtPublication = false
	if _, err := registry.Get(id); err != nil {
		t.Fatal("withdrawn authority deleted Connection")
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	workspace, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{"docs": {ConnectionID: id.String()}})
	if err != nil {
		t.Fatal(err)
	}
	attached, err := routing.NewConfig([]routing.Workspace{workspace})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.Update(t.Context(), func(routing.Config) (routing.Config, error) { return attached, nil }); err != nil {
		t.Fatal(err)
	}
	w := request([]string{`"1"`}, accessidentity.Local)
	var blocked struct {
		ControlProblem
		Consumers []connectionapp.Reference
	}
	if json.Unmarshal(w.Body.Bytes(), &blocked) != nil || w.Code != 409 || blocked.Code != "CONNECTION_IN_USE" || len(blocked.Consumers) != 1 || blocked.Consumers[0].Workspace != "dev" || blocked.Consumers[0].Mount != "docs" || blocked.Consumers[0].Kind != domain.MCP {
		t.Fatalf("incorrect blockers: %s", w.Body)
	}
	if len(registry.Audits()) != 0 {
		t.Fatal("rejected deletion audited")
	}
	empty, _ := routing.NewConfig(nil)
	if _, err := workspaces.Update(t.Context(), func(routing.Config) (routing.Config, error) { return empty, nil }); err != nil {
		t.Fatal(err)
	}
	if w := request([]string{`"1"`}, accessidentity.Local); w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if w := request([]string{`"1"`}, accessidentity.Local); w.Code != 404 {
		t.Fatal(w.Code)
	}
	reopened, err := connectionfixture.Open(filepath.Join(dir, "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Audits()) != 1 || reopened.Audits()[0].Action != "connection.delete" {
		t.Fatal("missing or duplicate durable deletion fact")
	}
	if _, err := reopened.Get(id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("deletion did not survive restart")
	}
}

func TestCredentialReplacementJSONRejectsAmbiguousIntent(t *testing.T) {
	for _, raw := range []string{
		`{"secret":"first","secret":"second","same_identity":true}`,
		`{"secret":"first","same_identity":true,"unknown":"private"}`,
		`{"secret":null,"same_identity":true}`, `{"secret":"first","same_identity":null}`,
		`{"secret":"first"}`, `{"same_identity":true}`, `{"secret":123,"same_identity":true}`,
	} {
		var body connectionCredentialReplacement
		if err := json.Unmarshal([]byte(raw), &body); err == nil {
			t.Fatalf("accepted ambiguous intent: %s", raw)
		}
	}
	var body connectionCredentialReplacement
	if err := json.Unmarshal([]byte(`{"secret":"synthetic","same_identity":true,"environment":"TOKEN"}`), &body); err != nil || body.Environment != "TOKEN" || !body.SameIdentity {
		t.Fatal("valid replacement rejected", err)
	}
}
