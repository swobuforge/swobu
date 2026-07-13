package bootstrap

import (
	"context"
	"testing"

	trafficevidencestore "github.com/swobuforge/swobu/internal/adapters/outbound/trafficevidence"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/platform/config"
)

func TestStatus_ReportsDegradedWhenRecentTerminalTrafficHasFailure(t *testing.T) {
	t.Parallel()

	endpoint := mustEndpoint(t, "alpha", "backend-a")
	store := trafficevidencestore.NewTrafficEventStore(trafficevidencestore.StoreConfig{})
	route, err := trafficevidence.NewRoute("backend-a", "")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	requestID, err := trafficevidence.ParseRequestID("req_degraded")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	event, err := trafficevidence.NewTerminalTrafficEvent(trafficevidence.TrafficEventInput{RequestID: requestID, Endpoint: "alpha",
		Route:        route,
		Result:       trafficevidence.ResultClassBackendError,
		StatusCode:   503,
		Timing:       trafficevidence.NewUnknownTiming(),
		AttemptCount: 1,
	})
	if err != nil {
		t.Fatalf("NewTerminalTrafficEvent returned error: %v", err)
	}
	store.Append(context.Background(), event)

	daemon := &Daemon{
		endpoints:         newEndpointCatalog("unused.yaml", config.RuntimeConfig{BindAddr: "127.0.0.1:0"}, []endpointintent.Endpoint{endpoint}),
		trafficEventStore: store,
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
	spec, err := endpointintent.ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	providerConfig, err := endpointintent.NewProviderConfig(ref, spec, "https://example.test/v1", "")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(parsedName, []endpointintent.ProviderConfig{providerConfig}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}
