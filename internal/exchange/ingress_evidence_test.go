package exchange

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
)

type recordingEvidenceSink struct {
	events []trafficevidence.TrafficEvent
}

func (s *recordingEvidenceSink) Append(_ context.Context, event trafficevidence.TrafficEvent) {
	s.events = append(s.events, event)
}

func TestRequestIngress_RecordsTerminalTrafficEvidenceOnSuccess(t *testing.T) {
	endpoint := testIngressEndpoint(t, "backend-a")
	sink := &recordingEvidenceSink{}
	ingress := RequestIngress{
		trafficEvidence: sink,
		runner:          withRuntime(bufferedProviderIngressResolver(nil)),
	}

	out, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "req-1",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"messages":[{"role":"user","content":"hi"}]}`)),
		ClientHandler:   trafficevidence.NormalizeClientHandler("Codex/1.2"),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("HandleRequestWithEndpoint returned error: %v", err)
	}
	if out.Response.Transport.Status != http.StatusOK {
		t.Fatalf("response status = %d, want %d", out.Response.Transport.Status, http.StatusOK)
	}
	if len(sink.events) != 1 {
		t.Fatalf("evidence events len = %d, want 1", len(sink.events))
	}
	event := sink.events[0]
	if got := event.Endpoint(); got != "alpha" {
		t.Fatalf("event endpoint = %q, want alpha", got)
	}
	if got := event.Result(); got != trafficevidence.ResultClassSuccess {
		t.Fatalf("event result = %q, want success", got)
	}
	if got := event.StatusCode(); got != http.StatusOK {
		t.Fatalf("event status code = %d, want %d", got, http.StatusOK)
	}
	if got := event.Route().ProviderConfigRef(); got != "backend-a" {
		t.Fatalf("event route provider ref = %q, want backend-a", got)
	}
	if got := event.Route().Model(); got != "m" {
		t.Fatalf("event route model = %q, want m", got)
	}
	if got := event.ClientFamily(); got != trafficevidence.ClientFamily("responses") {
		t.Fatalf("event client family = %q, want responses", got)
	}
	if got := event.ClientHandler(); got != trafficevidence.ClientHandler("Codex/1.2") {
		t.Fatalf("event client handler = %q, want codex", got)
	}
}

func TestRequestIngress_RecordsTerminalTrafficEvidenceOnBackendError(t *testing.T) {
	endpoint := testIngressSinglePathEndpoint(t, "backend-a")
	sink := &recordingEvidenceSink{}
	ingress := RequestIngress{
		trafficEvidence: sink,
		runner: withRuntime(func(context.Context, ProviderRequest) (ProviderIngress, error) {
			return nil, canonical.NewBackendError("backend-a", http.StatusServiceUnavailable, "backend-a unavailable", "")
		}),
	}

	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "req-2",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"messages":[{"role":"user","content":"hi"}]}`)),
		ClientHandler:   trafficevidence.NormalizeClientHandler("Claude-Code/2.0"),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err == nil {
		t.Fatal("HandleRequestWithEndpoint returned nil error, want backend error")
	}
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("HandleRequestWithEndpoint error = %v, want backend error", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("evidence events len = %d, want 1", len(sink.events))
	}
	event := sink.events[0]
	if got := event.Result(); got != trafficevidence.ResultClassBackendError {
		t.Fatalf("event result = %q, want backend_error", got)
	}
	if got := event.StatusCode(); got != http.StatusServiceUnavailable {
		t.Fatalf("event status code = %d, want %d", got, http.StatusServiceUnavailable)
	}
	if got := event.Route().ProviderConfigRef(); got != "backend-a" {
		t.Fatalf("event route provider ref = %q, want backend-a", got)
	}
}

func testIngressEndpoint(t *testing.T, selectedRef string) endpointintent.Endpoint {
	t.Helper()
	return testIngressEndpointWithPaths(t, []string{selectedRef}, selectedRef)
}

func testIngressSinglePathEndpoint(t *testing.T, selectedRef string) endpointintent.Endpoint {
	t.Helper()
	return testIngressEndpointWithPaths(t, []string{selectedRef}, selectedRef)
}

func testIngressEndpointWithRouteModel(t *testing.T, routeModelID, providerModelID string) endpointintent.Endpoint {
	t.Helper()
	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	ref, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	cfg, err := endpointintent.NewProviderConfig(ref, spec, "https://a.test/v1", "cred-a")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	cfg, err = cfg.WithRouteModelID(routeModelID)
	if err != nil {
		t.Fatalf("WithRouteModelID returned error: %v", err)
	}
	cfg, err = cfg.WithModelID(providerModelID)
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfg}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}

func testIngressEndpointWithPaths(t *testing.T, refs []string, selectedRef string) endpointintent.Endpoint {
	t.Helper()
	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	configs := make([]endpointintent.ProviderConfig, 0, len(refs))
	for _, refName := range refs {
		ref, err := endpointintent.ParseProviderConfigRef(refName)
		if err != nil {
			t.Fatalf("ParseProviderConfigRef(%s) returned error: %v", refName, err)
		}
		cfg, err := endpointintent.NewProviderConfig(ref, spec, "https://example.test/v1", "cred-"+refName)
		if err != nil {
			t.Fatalf("NewProviderConfig(%s) returned error: %v", refName, err)
		}
		cfg, err = cfg.WithProviderProtocol("responses")
		if err != nil {
			t.Fatalf("WithProviderProtocol(%s) returned error: %v", refName, err)
		}
		cfg, err = cfg.WithModelID("m")
		if err != nil {
			t.Fatalf("WithModelID(%s) returned error: %v", refName, err)
		}
		configs = append(configs, cfg)
	}
	selected, err := endpointintent.ParseProviderConfigRef(selectedRef)
	if err != nil {
		t.Fatalf("ParseProviderConfigRef(selected) returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, configs, selected)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}
