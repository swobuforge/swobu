package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/swobuforge/swobu/internal/search"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/routing"
)

// Exercise the customer path against a running Node. No Search runtime seam is
// injected: Operator authoring, grants, encrypted owner relay and upstream HTTP
// all use the production composition.
func TestSearchFallbackOperatorLocalRelayRotationAndRestart(t *testing.T) {
	t.Setenv("SWOBU_RELAY_ADDR", "127.0.0.1:1")
	t.Setenv("SWOBU_SEARCH_PROOF_OLD", "synthetic-old")
	t.Setenv("SWOBU_SEARCH_PROOF_NEW", "synthetic-new")
	root := t.TempDir()
	path := filepath.Join(root, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces:\n  research:\n    routes: []\n    enabled_services: [search]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var primaryCalls, secondaryCalls atomic.Int32
	var mode atomic.Int32
	var freshFails atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-old" {
			t.Error("old captured credential changed")
			w.WriteHeader(401)
			return
		}
		switch mode.Load() {
		case 0:
			http.Error(w, "unavailable", 503)
			return
		case 1:
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"results":[]}`)
			return
		case 2:
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"results":[{"url":"https://old.example.test","title":"old"}]}`)
	}))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondaryCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"results":[{"url":"https://fallback.example.test","title":"fallback"}]}`)
	}))
	defer secondary.Close()
	fresh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if freshFails.Load() {
			http.Error(w, "unavailable", 503)
			return
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-new" {
			t.Error("new revision used old credential")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"results":[{"url":"https://new.example.test","title":"new"}]}`)
	}))
	defer fresh.Close()
	input := StartInput{ConfigPath: path}
	input.StartupConfig.Addr = "127.0.0.1:0"
	daemon, err := Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { daemon.Close() })
	control := func(method, address, etag, key, body string, want int) ([]byte, string) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, daemon.BaseURL()+address, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Fatalf("%s %s status=%d want=%d: %s", method, address, resp.StatusCode, want, raw)
		}
		return raw, resp.Header.Get("ETag")
	}
	create := func(name, endpoint, credential string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"display_name": name, "kind": "search", "spec": map[string]any{"provider_id": "fixture", "endpoint": endpoint, "credential_ref": credential}})
		raw, _ := control("POST", "/api/v1/connections", "", "search-proof-"+name, string(body), 201)
		var result struct {
			Connection struct {
				ID string `json:"id"`
			} `json:"connection"`
		}
		if json.Unmarshal(raw, &result) != nil || result.Connection.ID == "" {
			t.Fatal("missing created Connection")
		}
		return result.Connection.ID
	}
	primaryID := create("primary", primary.URL, "env:SWOBU_SEARCH_PROOF_OLD")
	secondaryID := create("secondary", secondary.URL, "")
	_, etag := control("GET", "/api/v1/workspaces/research", "", "", "", 200)
	route := fmt.Sprintf(`{"connection_id":%q,"fallback_connection_ids":[%q]}`, primaryID, secondaryID)
	control("PUT", "/api/v1/workspaces/research/search-route", "", "", route, 428)
	control("PUT", "/api/v1/workspaces/research/search-route", etag, "", route, 200)
	control("PUT", "/api/v1/workspaces/research/search-route", etag, "", route, 412)
	_, currentETag := control("GET", "/api/v1/workspaces/research", "", "", "", 200)
	control("DELETE", "/api/v1/workspaces/research/services/search", currentETag, "", "", 409)
	raw, _ := control("GET", "/api/v1/workspaces/research/search-route", "", "", "", 200)
	if !strings.Contains(string(raw), secondaryID) {
		t.Fatal("fallback missing from readback")
	}
	control("DELETE", "/api/v1/connections/"+secondaryID, `"1"`, "", "", 409)
	workspace, _ := routing.ParseWorkspaceSlug("research")
	unlock := daemon.publicationStore.LockLifecycle()
	err = daemon.publicationStore.Set(workspace, true, daemon.publicationStore.Revision(), func() error { return nil })
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	principal, credential, err := daemon.accessStore.Create("search-user", workspace)
	if err != nil {
		t.Fatal(err)
	}
	send := func(ctx context.Context, client *http.Client, base string, want int) (string, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", base+"/workspaces/research/search?query=proof&limit=1", nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+credential.Token)
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		if resp.StatusCode != want {
			return "", fmt.Errorf("Search status=%d want=%d: %s", resp.StatusCode, want, raw)
		}
		return string(raw), nil
	}
	check := func(client *http.Client, base, contains string) {
		t.Helper()
		raw, err := send(t.Context(), client, base, 200)
		if err != nil || !strings.Contains(raw, contains) {
			t.Fatalf("Search response=%s err=%v", raw, err)
		}
	}
	check(http.DefaultClient, daemon.BaseURL(), "fallback")
	relay := ownerRelaySearchClient(t, daemon.publicHandler)
	check(relay, "https://owner.example", "fallback")
	if primaryCalls.Load() != 2 || secondaryCalls.Load() != 2 {
		t.Fatal("fallback skipped upstream execution")
	}
	events := daemon.searchEvidence.Events()
	if len(events) != 8 {
		t.Fatalf("attempt lifecycle facts=%d", len(events))
	}
	mode.Store(1)
	check(http.DefaultClient, daemon.BaseURL(), `"results":[]`)
	if secondaryCalls.Load() != 2 {
		t.Fatal("empty success retried")
	}
	mode.Store(2)
	type response struct {
		body string
		err  error
	}
	done := make(chan response, 1)
	go func() {
		raw, err := send(t.Context(), http.DefaultClient, daemon.BaseURL(), 200)
		done <- response{raw, err}
	}()
	select {
	case <-entered:
	case <-t.Context().Done():
		t.Fatal("in-flight request did not start")
	}
	edit, _ := json.Marshal(map[string]any{"display_name": "primary", "kind": "search", "same_identity": true, "spec": map[string]any{"provider_id": "fixture", "endpoint": fresh.URL, "credential_ref": "env:SWOBU_SEARCH_PROOF_NEW"}})
	control("PATCH", "/api/v1/connections/"+primaryID, `"1"`, "", string(edit), 200)
	check(http.DefaultClient, daemon.BaseURL(), "new.example.test")
	close(release)
	old := <-done
	if old.err != nil || !strings.Contains(old.body, "old.example.test") {
		t.Fatalf("in-flight revision changed: %s %v", old.body, old.err)
	}
	// One request, two upstream attempts: a two-request ceiling permits two
	// complete fallbacks, and denies the third at the common ingress owner.
	_, etag = control("GET", "/api/v1/workspaces/research", "", "", "", 200)
	control("PUT", "/api/v1/workspaces/research/service-limits", etag, "", `{"search":{"requests_per_minute":2}}`, 200)
	// Existing requests count toward the bucket, so restart resets only credits.
	freshFails.Store(true)
	beforeRestart := daemon.searchEvidence.Events()
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	daemon, err = Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	restoredEvents := daemon.searchEvidence.Events()
	if len(restoredEvents) != len(beforeRestart) {
		t.Fatalf("restart lost Search evidence: before=%d after=%d", len(beforeRestart), len(restoredEvents))
	}
	for i, event := range restoredEvents {
		if event.Operation != beforeRestart[i].Operation || event.Outcome != beforeRestart[i].Outcome || event.ConnectionRevision != beforeRestart[i].ConnectionRevision {
			t.Fatal("restart changed Search attempt identity/outcome")
		}
	}
	check(http.DefaultClient, daemon.BaseURL(), "fallback.example.test")
	check(ownerRelaySearchClient(t, daemon.publicHandler), "https://owner.example", "fallback.example.test")
	if _, err := send(t.Context(), http.DefaultClient, daemon.BaseURL(), 429); err != nil {
		t.Fatal(err)
	}
	raw, _ = control("GET", "/api/v1/workspaces/research/search-route", "", "", "", 200)
	if !strings.Contains(string(raw), secondaryID) {
		t.Fatal("restart lost fallback")
	}
	events = daemon.searchEvidence.Events()[len(beforeRestart):]
	if len(events) != 8 || events[1].ConnectionRevision != "2" || events[3].ConnectionRevision != "1" || events[5].ConnectionRevision != "2" || events[7].ConnectionRevision != "1" {
		t.Fatalf("fresh attempt revisions: %+v", events)
	}
	_, etag = control("GET", "/api/v1/workspaces/research", "", "", "", 200)
	control("PUT", "/api/v1/workspaces/research/service-limits", etag, "", `{"search":{"requests_per_minute":0}}`, 200)
	permission, err := daemon.accessStore.PermissionRevision(principal.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	_, err = daemon.accessStore.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, workspace, permission, "search-rate-proof", func() error { return nil }, []routing.ServiceKind{routing.ServiceSearch}, []accessstate.RequestRateLimit{{Service: routing.ServiceSearch, RequestsPerMinute: 1}})
	if err != nil {
		t.Fatal(err)
	}
	check(http.DefaultClient, daemon.BaseURL(), "fallback.example.test")
	callsBeforeDenied := secondaryCalls.Load()
	if _, err := send(t.Context(), ownerRelaySearchClient(t, daemon.publicHandler), "https://owner.example", 429); err != nil {
		t.Fatal(err)
	}
	if secondaryCalls.Load() != callsBeforeDenied {
		t.Fatal("Principal GET ceiling reached upstream")
	}
	_, etag = control("GET", "/api/v1/workspaces/research", "", "", "", 200)
	control("DELETE", "/api/v1/workspaces/research/search-route", etag, "", "", 200)
	control("GET", "/api/v1/workspaces/research/search-route", "", "", "", 404)
	control("DELETE", "/api/v1/connections/"+secondaryID, `"1"`, "", "", 204)
}

func TestSearchRetentionFailureDegradesStatusWithoutControllingExecution(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces:\n  research:\n    routes: []\n    enabled_services: [search]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	history := filepath.Join(root, "swobu.search-evidence.json")
	if err := os.WriteFile(history, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	input := StartInput{ConfigPath: path, SearchService: search.Service{ConnectionIDs: func(string) ([]string, bool) { return []string{"fixture"}, true }, Sources: map[string]search.Source{"fixture": search.StaticSource{{Title: "query"}}}}}
	input.StartupConfig.Addr = "127.0.0.1:0"
	daemon, err := Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	status, err := daemon.Status()
	if err != nil || status.State != HealthStateDegraded {
		t.Fatal("failed Search retention was hidden in Node status")
	}
	results, err := daemon.searchService.Search(t.Context(), "research", search.Query{Text: "query"})
	if err != nil || len(results) != 1 {
		t.Fatalf("retention changed execution: %v %v", results, err)
	}
	if err := daemon.Close(); !errors.Is(err, search.ErrEvidenceRetention) {
		t.Fatal("shutdown hid retention failure", err)
	}
	raw, err := os.ReadFile(history)
	if err != nil || string(raw) != "corrupt" {
		t.Fatal("corrupt retained history was overwritten")
	}
}
