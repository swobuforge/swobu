package runtime_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	operatormodelcatalog "github.com/metrofun/swobu/internal/app/operator/modelcatalog"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
)

func TestRuntimeBootstrap_ServesOperatorModelCatalog(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/v1/models")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1-mini"},{"id":"gpt-4.1"}]}`))
	}))
	defer upstream.Close()

	daemon := startDaemon(t, runtimeFixture{
		endpoints: []endpointintent.Endpoint{
			testEndpoint(t, "alpha", "backend-a", testProviderConfig(t, "backend-a", "custom", upstream.URL+"/v1", "", protocolsurface.ChatCompletions)),
		},
	})
	defer func() { _ = daemon.Close(context.Background()) }()

	resp, err := http.Get(daemon.BaseURL() + "/_swobu/model-catalog")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var snapshot operatormodelcatalog.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if len(snapshot.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(snapshot.Entries))
	}
	if got := snapshot.Entries[0].EndpointName; got != "alpha" {
		t.Fatalf("endpoint name = %q, want %q", got, "alpha")
	}
	if got := snapshot.Entries[0].ModelIDs[0]; got != "gpt-4.1" {
		t.Fatalf("first model id = %q, want sorted %q", got, "gpt-4.1")
	}
}
