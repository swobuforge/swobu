package bootstrap

import (
	"context"
	"testing"

	evidencestore "github.com/swobuforge/swobu/internal/adapters/outbound/evidence"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/domain/runtimeevidence"
	"github.com/swobuforge/swobu/internal/platform/config"
)

func TestStatus_ReportsDegradedWhenRecentTerminalTrafficHasFailure(t *testing.T) {
	t.Parallel()

	endpoint := mustEndpoint(t, "alpha", "backend-a")
	evidence := evidencestore.NewStore(evidencestore.StoreConfig{})
	route, err := runtimeevidence.NewRoute("backend-a", "")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	requestID, err := runtimeevidence.ParseRequestID("req_degraded")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	event, err := runtimeevidence.NewTerminalTrafficEvent(runtimeevidence.TrafficEventInput{
		RequestID:    requestID,
		Endpoint:     "alpha",
		Route:        route,
		Result:       runtimeevidence.ResultClassBackendError,
		StatusCode:   503,
		Timing:       runtimeevidence.NewUnknownTiming(),
		AttemptCount: 1,
	})
	if err != nil {
		t.Fatalf("NewTerminalTrafficEvent returned error: %v", err)
	}
	evidence.Append(context.Background(), event)

	daemon := &Daemon{
		endpoints: newEndpointCatalog("unused.yaml", config.RuntimeConfig{BindAddr: "127.0.0.1:0"}, []endpointintent.Endpoint{endpoint}),
		evidence:  evidence,
	}
	status, err := daemon.Status()
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if status.State != HealthStateDegraded {
		t.Fatalf("state = %q, want %q", status.State, HealthStateDegraded)
	}
}

func mustEndpoint(t *testing.T, name string, selectedRef string) endpointintent.Endpoint {
	t.Helper()

	parsedName, err := endpointintent.ParseEndpointName(name)
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	ref, err := endpointintent.ParseProviderConfigRef(selectedRef)
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	providerConfig, err := endpointintent.NewProviderConfig(ref, spec, "https://example.test/v1", "", protocolsurface.ChatCompletions)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(parsedName, []endpointintent.ProviderConfig{providerConfig}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}
