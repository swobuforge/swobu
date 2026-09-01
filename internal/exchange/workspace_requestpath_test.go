package exchange

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

type channelTrafficEventSink struct {
	events chan trafficevidence.TrafficEvent
}

func (s channelTrafficEventSink) Append(_ context.Context, event trafficevidence.TrafficEvent) {
	s.events <- event
}

func requestpathTarget(t *testing.T, id string) routing.Target {
	t.Helper()
	targetID, _ := routing.ParseTargetID(id)
	model, _ := routing.ParseUpstreamModel("upstream-" + id)
	provider, _ := routing.ParseProvider("custom", func(candidate string) bool { return candidate == "custom" })
	connection, _ := routing.NewCustomConnection(provider, "https://example.test/v1", nil)
	protocol, _ := routing.ParseProtocol("responses", provider, func(routing.Provider, string) bool { return true })
	target, err := routing.NewTarget(targetID, model, protocol, connection)
	if err != nil {
		t.Fatal(err)
	}
	return target
}
func requestpathWorkspace(t *testing.T) routing.Workspace {
	t.Helper()
	slug, _ := routing.ParseWorkspaceSlug("dev")
	aName, _ := routing.ParseRouteName("a")
	bName, _ := routing.ParseRouteName("b")
	aTier, _ := routing.NewTier([]routing.Target{requestpathTarget(t, "target-a")})
	bTier, _ := routing.NewTier([]routing.Target{requestpathTarget(t, "target-b")})
	a, _ := routing.NewRoute(aName, []routing.Tier{aTier})
	b, _ := routing.NewRoute(bName, []routing.Tier{bTier})
	workspace, err := routing.NewWorkspace(slug, aName, []routing.Route{a, b})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func TestRequestPathPublishesInflightEvidenceBeforeProviderReturns(t *testing.T) {
	workspace := requestpathWorkspace(t)
	release := make(chan struct{})
	providerEntered := make(chan struct{})
	sink := channelTrafficEventSink{events: make(chan trafficevidence.TrafficEvent, 1)}
	runner := withRuntime(func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		close(providerEntered)
		<-release
		return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(`{"id":"resp","model":"m","output_text":"ok"}`), carrier.Meta{})}, nil
	})
	runner.TrafficEvidence = sink

	done := make(chan error, 1)
	go func() {
		_, err := runExchange(context.Background(), runner, "req_inflight_live", "codex-tui/0.147.0", canonical.ClientFamilyResponses, delivery.BufferedDelivery(), testDecodedRequest(testCanonicalRequest("a")), nil, workspace, nil, canonical.NormalizedPathResponses)
		done <- err
	}()

	select {
	case event := <-sink.events:
		if event.Result() != trafficevidence.ResultClassInProgress || event.EventKind() != trafficevidence.EventKindProviderInflight {
			t.Fatalf("in-flight kind/result = %q/%q", event.EventKind(), event.Result())
		}
		if event.WorkspaceRouteModelID() != "a" || event.ProviderSpec() != "custom" || event.ProviderModel() != "upstream-target-a" {
			t.Fatalf("route/target evidence = %q %q/%q", event.WorkspaceRouteModelID(), event.ProviderSpec(), event.ProviderModel())
		}
		if event.StatusCode() != 0 {
			t.Fatalf("in-flight status = %d, want none", event.StatusCode())
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for in-flight evidence before provider returned")
	}

	select {
	case <-providerEntered:
	case <-time.After(time.Second):
		t.Fatal("provider transport was not entered")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRequestPathNeverAttemptsTargetFromAnotherRoute(t *testing.T) {
	workspace := requestpathWorkspace(t)
	var refs []string
	ingress := RequestIngress{runner: withRuntime(func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		refs = append(refs, target.TargetID)
		return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(`{"id":"resp","model":"m","output_text":"ok"}`), carrier.Meta{})}, nil
	})}
	_, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, RequestInput{ExchangeID: "route-a", Request: NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"a","input":"hi"}`)), ClientFamily: canonical.ClientFamilyResponses, ResponseFraming: delivery.FramingSSE})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0] != "target-a" {
		t.Fatalf("attempted targets=%v", refs)
	}
}

func TestRequestPathProjectedRouteCannotEscapeOneRouteWorkspace(t *testing.T) {
	source := requestpathWorkspace(t)
	sharedRouteName, err := routing.ParseRouteName("b")
	if err != nil {
		t.Fatal(err)
	}
	sharedRoute, err := source.ResolveRoute(sharedRouteName.String())
	if err != nil {
		t.Fatal(err)
	}
	projected, err := routing.NewWorkspace(source.Slug(), sharedRouteName, []routing.Route{sharedRoute})
	if err != nil {
		t.Fatal(err)
	}

	var refs []string
	ingress := RequestIngress{runner: withRuntime(func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		refs = append(refs, target.TargetID)
		return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(`{"id":"resp","model":"m","output_text":"ok"}`), carrier.Meta{})}, nil
	})}
	_, err = ingress.HandleRequestWithWorkspace(context.Background(), projected, RequestInput{
		ExchangeID:      "shared-route",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"a","input":"hi"}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0] != "target-b" {
		t.Fatalf("projected route attempted targets=%v, want target-b only", refs)
	}
}

func TestRequestPathFixedClientModelUsesConfiguredDefaultRoute(t *testing.T) {
	workspace := requestpathWorkspace(t)
	var refs []string
	ingress := RequestIngress{runner: withRuntime(func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		refs = append(refs, target.TargetID)
		return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(`{"id":"resp","model":"m","output_text":"ok"}`), carrier.Meta{})}, nil
	})}
	_, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, RequestInput{ExchangeID: "fixed-model", Request: NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"client-owned-model","input":"hi"}`)), ClientFamily: canonical.ClientFamilyResponses, ResponseFraming: delivery.FramingSSE})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0] != "target-a" {
		t.Fatalf("fixed client model attempted targets=%v, want target-a", refs)
	}
}

func TestRequestPathPublicDefaultModelAttemptsConfiguredDefaultRoute(t *testing.T) {
	workspace := requestpathWorkspace(t)
	var refs []string
	ingress := RequestIngress{runner: withRuntime(func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		refs = append(refs, target.TargetID)
		return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(`{"id":"resp","model":"m","output_text":"ok"}`), carrier.Meta{})}, nil
	})}
	_, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, RequestInput{ExchangeID: "default", Request: NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"default","input":"hi"}`)), ClientFamily: canonical.ClientFamilyResponses, ResponseFraming: delivery.FramingSSE})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0] != "target-a" {
		t.Fatalf("default model attempted targets=%v, want target-a", refs)
	}
}
