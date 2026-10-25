package bootstrap

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/search"
)

// A real running Node observes Search Connection creation/edit/revision at the
// next request, not only after restart. Fallback executes through the same
// Workspace endpoint; no parallel routing or additional authority is needed.
func TestLiveSearchConnectionFailoverAndRevisionChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "swobu.yaml")
	if err := os.WriteFile(path, []byte(`schema_version: 5
workspaces:
  research:
    routes: []
    search:
      connection_id: cn_primary
      fallback_connection_ids: [cn_secondary]
`), 0600); err != nil {
		t.Fatal(err)
	}
	var primaryCalls, fallbackCalls atomic.Int32
	primaryFail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer primaryFail.Close()
	primaryRecover := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"url":"https://primary.example.test","title":"primary"}]}`)
	}))
	defer primaryRecover.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"url":"https://fallback.example.test","title":"fallback"}]}`)
	}))
	defer fallback.Close()
	build := func(idRaw, endpointRaw string) connections.Connection {
		t.Helper()
		id, err := connectionidentity.Parse(idRaw)
		if err != nil {
			t.Fatal(err)
		}
		endpoint, err := connections.ParseEndpoint(endpointRaw)
		if err != nil {
			t.Fatal(err)
		}
		spec, err := connections.NewSearchSpec("fixture", endpoint, credentialref.Ref{})
		if err != nil {
			t.Fatal(err)
		}
		value, err := connections.New(id, idRaw, spec, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	original := build("cn_primary", primaryFail.URL)
	secondary := build("cn_secondary", fallback.URL)
	publishCurrentConnectionFixtures(t, path, original, secondary)
	daemon, err := Start(t.Context(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	query := search.Query{Text: "query", Limit: 1}
	results, err := daemon.searchService.Search(t.Context(), "research", query)
	if err != nil || len(results) != 1 || !strings.Contains(results[0].URL, "fallback") || primaryCalls.Load() != 1 || fallbackCalls.Load() != 1 {
		t.Fatalf("fallback result=%v err=%v primary=%d fallback=%d", results, err, primaryCalls.Load(), fallbackCalls.Load())
	}
	endpoint, err := connections.ParseEndpoint(primaryRecover.URL)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewSearchSpec("fixture", endpoint, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := original.EditSameIdentity(original.Revision(), "updated primary", spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.connectionStore.Publish(rotated, original.Revision()); err != nil {
		t.Fatal(err)
	}
	currentValue, getErr := daemon.connectionStore.Get(original.ID())
	if getErr != nil {
		t.Fatal(getErr)
	}
	if currentValue.Revision() != rotated.Revision() || currentValue.Spec().(connections.SearchSpec).Endpoint() != endpoint {
		t.Fatal("store did not publish edited endpoint/revision")
	}
	if _, openErr := search.OpenConnectionBinding(t.Context(), currentValue, nil, nil); openErr != nil {
		t.Fatalf("direct binding: %v", openErr)
	}
	results, err = daemon.searchService.Search(t.Context(), "research", query)
	if err != nil || len(results) != 1 || !strings.Contains(results[0].URL, "primary") || fallbackCalls.Load() != 1 {
		t.Fatalf("rotated Search source not effective without restart: result=%v err=%v fallback=%d", results, err, fallbackCalls.Load())
	}
	// A missing current credential never resurrects the old revision or blocks
	// the independent fallback provider.
	brokenSpec, err := connections.NewSearchSpec("fixture", endpoint, credentialref.Parse("env:SWOBU_MISSING_SEARCH_REVISION"))
	if err != nil {
		t.Fatal(err)
	}
	broken, err := rotated.EditSameIdentity(rotated.Revision(), "broken primary", brokenSpec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SWOBU_MISSING_SEARCH_REVISION", "")
	if err := daemon.connectionStore.Publish(broken, rotated.Revision()); err != nil {
		t.Fatal(err)
	}
	results, err = daemon.searchService.Search(t.Context(), "research", query)
	if err != nil || len(results) != 1 || !strings.Contains(results[0].URL, "fallback") || fallbackCalls.Load() != 2 {
		t.Fatalf("failed revision resurrected previous or blocked fallback: %v %v", results, err)
	}
	// Revoked or cancelled work must not escape into another attempt.
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = daemon.searchService.Search(cancelled, "research", query)
	if !errors.Is(err, context.Canceled) || fallbackCalls.Load() != 2 {
		t.Fatalf("cancellation escaped err=%v fallback=%d", err, fallbackCalls.Load())
	}
}
