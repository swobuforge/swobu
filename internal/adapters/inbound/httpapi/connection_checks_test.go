package httpapi

import (
	"context"
	"encoding/json"
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

func TestConnectionChecksReviewedSnapshotAndEphemeralReadback(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := connectionfixture.Open(filepath.Join(root, "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := configstore.OpenOrCreate(filepath.Join(root, "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer workspaces.Close()
	endpoint, _ := domain.ParseEndpoint("https://example.test/mcp")
	spec, _ := domain.NewMCPHTTPSpec(endpoint, credentialref.Parse("env:PRIVATE_LOCATOR"))
	id, _ := connectionidentity.New()
	value, err := domain.New(id, "Shared", spec, time.Now().Add(-time.Minute))
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
	probed := 0
	editDuringProbe := false
	probe := func(ctx context.Context, snapshot domain.Connection) (domain.Check, error) {
		probed++
		if snapshot.ID() != id || snapshot.Revision() != value.Revision() {
			t.Error("wrong snapshot")
		}
		if editDuringProbe {
			updated, err := value.EditSameIdentity(value.Revision(), "Edited", spec, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := registry.Publish(updated, value.Revision()); err != nil {
				t.Fatal(err)
			}
		}
		return domain.NewCheck(snapshot, time.Now(), domain.Reachable, domain.AuthenticationAccepted, []domain.Capability{domain.CapabilityTools}, domain.CheckOK)
	}
	handler := NewConnectionControlHandler(queries, commands, nil, probe)
	path := "/api/v1/connections/" + id.String() + "/checks"
	request := func(revision, body string, trusted bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		if revision != "" {
			r.Header.Set("If-Match", revision)
		}
		if trusted {
			r = r.WithContext(contextWithAuthority(r, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) { return accessidentity.Local, nil })))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, test := range []struct {
		revision, body string
		trusted        bool
		status         int
	}{
		{`"1"`, "", false, 403}, {"", "", true, 428}, {`W/"1"`, "", true, 400}, {`"2"`, "", true, 412}, {`"1"`, `{}`, true, 400},
	} {
		if got := request(test.revision, test.body, test.trusted); got.Code != test.status {
			t.Fatalf("status=%d want=%d body=%s", got.Code, test.status, got.Body.String())
		}
	}
	if probed != 0 {
		t.Fatal("probed rejected request")
	}
	response := request(`"1"`, "", true)
	var check connectionapp.Check
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &check) != nil || check.ConnectionID != id.String() || check.ConnectionRevision != "1" || response.Header().Get("ETag") != "" || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "PRIVATE_LOCATOR") {
		t.Fatal(response.Code, response.Body.String())
	}
	detail, err := queries.Get(t.Context(), id)
	if err != nil || detail.Revision != "1" || detail.LatestCheck == nil {
		t.Fatal(detail, err)
	}
	reopened, err := connectionfixture.Open(filepath.Join(root, "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.LatestCheck(id); ok {
		t.Fatal("persisted health")
	}
	editDuringProbe = true
	response = request(`"1"`, "", true)
	if response.Code != 412 || !strings.Contains(response.Body.String(), `"revision":"2"`) {
		t.Fatal(response.Code, response.Body.String())
	}
	if _, ok := registry.LatestCheck(id); ok {
		t.Fatal("stale probe published")
	}
}
