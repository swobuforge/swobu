package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/testkit/connectionfixture"
)

func TestJSONSourceQueriesFixedEndpointAndProjectsProvenance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "swobu" || r.URL.Query().Get("format") != "json" {
			t.Fatalf("query=%s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"url":"https://example.test/a","title":"Swobu","content":"result"}]}`))
	}))
	defer server.Close()
	source, err := NewJSONSource(server.URL+"?language=en", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	results, err := source.Search(context.Background(), Query{Text: "swobu", Limit: 1})
	if err != nil || len(results) != 1 || results[0].Source == "" || results[0].Snippet != "result" {
		t.Fatalf("results=%#v err=%v", results, err)
	}
}

func TestJSONSourceRejectsMalformedAndOversizedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("not-json")) }))
	defer server.Close()
	source, err := NewJSONSource(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Search(context.Background(), Query{Text: "x", Limit: 1}); err == nil {
		t.Fatal("malformed response accepted")
	}
	source.MaxResponseBytes = 4
	if _, err := source.Search(context.Background(), Query{Text: "x", Limit: 1}); err == nil {
		t.Fatal("oversized response accepted")
	}
	if _, err := NewJSONSource("file:///tmp/search", nil); err == nil {
		t.Fatal("non-HTTP endpoint accepted")
	}
	if strings.Contains(source.Endpoint.String(), "credential") {
		t.Fatal("unexpected credential in endpoint")
	}
}

func TestPersistedSearchConnectionKeepsCleanEndpointOnWire(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("q") != "synthetic" || r.URL.Fragment != "" {
			t.Error("Connection registry changed executed Search URL")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()
	endpoint := server.URL + "/search"
	parsedEndpoint, err := connections.ParseEndpoint(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewSearchSpec("search", parsedEndpoint, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.Parse("cn_search")
	at := time.Now().UTC()
	value, err := connections.New(id, "Search", spec, at)
	if err != nil {
		t.Fatal(err)
	}
	// Test the normal registry persistence, not historical migration encoding.
	registryPath := filepath.Join(t.TempDir(), "swobu.yaml")
	registry, err := connectionfixture.Open(registryPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(value, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
	node, err := nodefile.Open(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := node.ConnectionDocument()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := connections.ValidateDocument(raw, nil)
	if err != nil || len(restored) != 1 {
		t.Fatal("candidate failed reopen", err)
	}
	after := restored[0].Spec().(connections.SearchSpec)
	if after.Endpoint().String() != endpoint {
		t.Fatal("persisted endpoint changed")
	}
	for _, url := range []string{endpoint, after.Endpoint().String()} {
		source, err := NewJSONSource(url, server.Client())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := source.Search(t.Context(), Query{Text: "synthetic", Limit: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatal("expected before/after requests")
	}
}
