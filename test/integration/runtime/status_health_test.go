package runtime_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	httpapi "github.com/metrofun/swobu/internal/adapters/inbound/httpapi"
	"github.com/metrofun/swobu/internal/app/operator/controlplane"
	"github.com/metrofun/swobu/internal/bootstrap"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
)

func TestRuntimeStatus_ReportsUninitializedWhenEndpointSetIsEmpty(t *testing.T) {
	daemon := startDaemon(t, runtimeFixture{})
	defer func() { _ = daemon.Close(context.Background()) }()

	status, err := daemon.Status()
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if status.State != bootstrap.HealthStateUninitialized {
		t.Fatalf("state = %q, want %q", status.State, bootstrap.HealthStateUninitialized)
	}
	if status.EndpointCount != 0 {
		t.Fatalf("endpoint_count = %d, want 0", status.EndpointCount)
	}
}

func TestRuntimeStatus_ReportsHealthyAndServesStatusJSON(t *testing.T) {
	daemon := startDaemon(t, runtimeFixture{
		endpoints: []endpointintent.Endpoint{
			testEndpoint(t, "alpha", "backend-a", testProviderConfig(t, "backend-a", "custom", "https://example.test/v1", "", protocolsurface.ChatCompletions)),
		},
	})
	defer func() { _ = daemon.Close(context.Background()) }()

	status, err := daemon.Status()
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if status.State != bootstrap.HealthStateHealthy {
		t.Fatalf("state = %q, want %q", status.State, bootstrap.HealthStateHealthy)
	}
	if status.EndpointCount != 1 {
		t.Fatalf("endpoint_count = %d, want 1", status.EndpointCount)
	}

	resp, err := http.Get(daemon.BaseURL() + "/_swobu/status")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got httpapi.StatusDocument
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if got.State != string(bootstrap.HealthStateHealthy) {
		t.Fatalf("json state = %q, want %q", got.State, bootstrap.HealthStateHealthy)
	}
	if got.EndpointCount != 1 {
		t.Fatalf("json endpoint_count = %d, want 1", got.EndpointCount)
	}
	if got.ControlPlaneProtocol != controlplane.Protocol {
		t.Fatalf("json control_plane_protocol = %d, want %d", got.ControlPlaneProtocol, controlplane.Protocol)
	}
	if got.SwobuVersion != controlplane.SwobuVersion() {
		t.Fatalf("json swobu_version = %q, want %q", got.SwobuVersion, controlplane.SwobuVersion())
	}
}
