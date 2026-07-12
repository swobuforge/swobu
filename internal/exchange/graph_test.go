package exchange

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/observation"
)

func TestPortLinkAndResultCarryTypedValueAndEffects(t *testing.T) {
	t.Parallel()

	from := NewPort[string]("exchange.in")
	if from.IsZero() {
		t.Fatal("port should not be zero")
	}
	if got := from.ID(); got != "exchange.in" {
		t.Fatalf("port id = %q, want %q", got, "exchange.in")
	}

	link := NewLink(
		LinkID("exchange.test.link"),
		from,
		NewPort[string]("exchange.out"),
		func(_ context.Context, input string) (Result[string], error) {
			return NewResult(
				input+"-done",
				effect.ObservationEffect{Observation: observation.ObservationRecord{Code: "graph_link"}},
			), nil
		},
	)

	result, err := link.Run(context.Background(), "ping")
	if err != nil {
		t.Fatalf("link.Run error = %v", err)
	}
	if got := result.Value; got != "ping-done" {
		t.Fatalf("result value = %q, want %q", got, "ping-done")
	}
	if len(result.Effects) != 1 {
		t.Fatalf("result effects len = %d, want 1", len(result.Effects))
	}
	if got := result.Effects[0].Kind(); got != effect.KindObservation {
		t.Fatalf("effect kind = %q, want %q", got, effect.KindObservation)
	}
}

type providerIngressResolverFunc func(context.Context, ProviderRequest) (ProviderIngress, error)

func (f providerIngressResolverFunc) ResolveProviderIngress(ctx context.Context, req ProviderRequest) (ProviderIngress, error) {
	return f(ctx, req)
}

func TestExchangeGraph_BuildPathMaterializesModelBeforeProtocolResolution(t *testing.T) {
	_, endpoint := testGraphEndpoint(t)
	graph := exchangeGraph{
		DeliverySelector: FixedDeliverySelector{},
		Continuation:     canonical.NewContinuationRuntime(nil),
	}

	path, err := graph.buildPath(context.Background(), "ex_graph", endpoint.Name(), endpoint.SelectedProviderConfig(), delivery.BufferedDelivery(), testCanonicalRequest("m"))
	if err != nil {
		t.Fatalf("buildPath() error = %v", err)
	}
	if got := path.Request.Model(); got != "provider-model-a" {
		t.Fatalf("path request model = %q, want provider-model-a", got)
	}
	if got := path.Target.BackendRef; got != "backend-a" {
		t.Fatalf("path target backend ref = %q, want backend-a", got)
	}
	if got := path.ProtocolKind; got != protocolkind.Responses {
		t.Fatalf("path protocol kind = %q, want responses", got)
	}
}

func TestExchangeGraph_ExecutesPathsUntilSuccess(t *testing.T) {
	ctx := context.Background()
	_, endpoint := testGraphEndpoint(t)
	calls := make([]string, 0, 2)
	resolver := providerIngressResolverFunc(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		calls = append(calls, req.Target.BackendRef)
		switch req.Target.BackendRef {
		case "backend-a":
			return nil, canonical.NewBackendError(req.Target.BackendRef, http.StatusServiceUnavailable, "backend-a unavailable", "")
		case "backend-b":
			return carrier.NewWireDocument(
				carrier.StageProviderIngressIn,
				req.Target.ProtocolKind,
				"application/json",
				nil,
				[]byte(`{"id":"resp_1","model":"provider-model-b","output_text":"ok"}`),
				carrier.Meta{},
			), nil
		default:
			return nil, canonical.InternalError("unexpected backend target")
		}
	})

	graph := exchangeGraph{
		DeliverySelector: FixedDeliverySelector{},
		Continuation:     canonical.NewContinuationRuntime(nil),
		Runner:           withRuntime(resolver),
	}

	response, target, err := graph.Execute(ctx, exchangeGraphInput{
		ExchangeID:     "ex_graph",
		ClientFamily:   canonical.ClientFamilyResponses,
		ClientDelivery: delivery.BufferedDelivery(),
		Request:        testCanonicalRequest("m"),
		Endpoint:       endpoint,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := target.BackendRef; got != "backend-b" {
		t.Fatalf("target backend ref = %q, want backend-b", got)
	}
	if !reflect.DeepEqual(calls, []string{"backend-a", "backend-b"}) {
		t.Fatalf("calls = %#v, want backend-a then backend-b", calls)
	}
	raw, err := io.ReadAll(response.Transport.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if !strings.Contains(string(raw), "ok") {
		t.Fatalf("response body = %s, want ok", raw)
	}
}

func TestExchangeGraph_SkipsFallbackableMaterializationFailureBeforeSuccess(t *testing.T) {
	ctx := context.Background()
	_, endpoint := testGraphEndpointWithMissingModelFirstPath(t)
	calls := make([]string, 0, 1)
	resolver := providerIngressResolverFunc(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		calls = append(calls, req.Target.BackendRef)
		switch req.Target.BackendRef {
		case "backend-b":
			return carrier.NewWireDocument(
				carrier.StageProviderIngressIn,
				req.Target.ProtocolKind,
				"application/json",
				nil,
				[]byte(`{"id":"resp_1","model":"provider-model-b","output_text":"ok"}`),
				carrier.Meta{},
			), nil
		default:
			return nil, canonical.InternalError("unexpected backend target")
		}
	})

	graph := exchangeGraph{
		DeliverySelector: FixedDeliverySelector{},
		Continuation:     canonical.NewContinuationRuntime(nil),
		Runner:           withRuntime(resolver),
	}

	response, target, err := graph.Execute(ctx, exchangeGraphInput{
		ExchangeID:     "ex_graph_missing_model",
		ClientFamily:   canonical.ClientFamilyResponses,
		ClientDelivery: delivery.BufferedDelivery(),
		Request:        testCanonicalRequest("m"),
		Endpoint:       endpoint,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := target.BackendRef; got != "backend-b" {
		t.Fatalf("target backend ref = %q, want backend-b", got)
	}
	if !reflect.DeepEqual(calls, []string{"backend-b"}) {
		t.Fatalf("calls = %#v, want backend-b only", calls)
	}
	raw, err := io.ReadAll(response.Transport.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if !strings.Contains(string(raw), "ok") {
		t.Fatalf("response body = %s, want ok", raw)
	}
}

func TestExchangeGraph_AdvancesPastUnsupportedDeliveryFailure(t *testing.T) {
	ctx := context.Background()
	_, endpoint := testGraphEndpoint(t)
	calls := make([]string, 0, 2)
	resolver := providerIngressResolverFunc(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		calls = append(calls, req.Target.BackendRef)
		switch req.Target.BackendRef {
		case "backend-a":
			return nil, canonical.UnsupportedDelivery("backend-a does not support the requested delivery")
		case "backend-b":
			return carrier.NewWireDocument(
				carrier.StageProviderIngressIn,
				req.Target.ProtocolKind,
				"application/json",
				nil,
				[]byte(`{"id":"resp_1","model":"provider-model-b","output_text":"ok"}`),
				carrier.Meta{},
			), nil
		default:
			return nil, canonical.InternalError("unexpected backend target")
		}
	})

	graph := exchangeGraph{
		DeliverySelector: FixedDeliverySelector{},
		Continuation:     canonical.NewContinuationRuntime(nil),
		Runner:           withRuntime(resolver),
	}

	response, target, err := graph.Execute(ctx, exchangeGraphInput{
		ExchangeID:     "ex_graph_unsupported_delivery",
		ClientFamily:   canonical.ClientFamilyResponses,
		ClientDelivery: delivery.BufferedDelivery(),
		Request:        testCanonicalRequest("m"),
		Endpoint:       endpoint,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := target.BackendRef; got != "backend-b" {
		t.Fatalf("target backend ref = %q, want backend-b", got)
	}
	if !reflect.DeepEqual(calls, []string{"backend-a", "backend-b"}) {
		t.Fatalf("calls = %#v, want backend-a then backend-b", calls)
	}
	raw, err := io.ReadAll(response.Transport.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if !strings.Contains(string(raw), "ok") {
		t.Fatalf("response body = %s, want ok", raw)
	}
}

func testGraphEndpoint(t *testing.T) (endpointintent.EndpointName, endpointintent.Endpoint) {
	t.Helper()

	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}

	refA, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef(backend-a) returned error: %v", err)
	}
	refB, err := endpointintent.ParseProviderConfigRef("backend-b")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef(backend-b) returned error: %v", err)
	}

	cfgA, err := endpointintent.NewProviderConfig(refA, spec, "https://backend-a.test/v1", "cred-a")
	if err != nil {
		t.Fatalf("NewProviderConfig(backend-a) returned error: %v", err)
	}
	cfgA, err = cfgA.WithProviderProtocol("responses")
	if err != nil {
		t.Fatalf("WithProviderProtocol(backend-a) returned error: %v", err)
	}
	cfgA, err = cfgA.WithModelID("provider-model-a")
	if err != nil {
		t.Fatalf("WithModelID(backend-a) returned error: %v", err)
	}

	cfgB, err := endpointintent.NewProviderConfig(refB, spec, "https://backend-b.test/v1", "cred-b")
	if err != nil {
		t.Fatalf("NewProviderConfig(backend-b) returned error: %v", err)
	}
	cfgB, err = cfgB.WithProviderProtocol("responses")
	if err != nil {
		t.Fatalf("WithProviderProtocol(backend-b) returned error: %v", err)
	}
	cfgB, err = cfgB.WithModelID("provider-model-b")
	if err != nil {
		t.Fatalf("WithModelID(backend-b) returned error: %v", err)
	}

	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfgB, cfgA}, refA)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return name, endpoint
}

func testGraphEndpointWithMissingModelFirstPath(t *testing.T) (endpointintent.EndpointName, endpointintent.Endpoint) {
	t.Helper()

	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}

	refA, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef(backend-a) returned error: %v", err)
	}
	refB, err := endpointintent.ParseProviderConfigRef("backend-b")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef(backend-b) returned error: %v", err)
	}

	cfgA, err := endpointintent.NewProviderConfig(refA, spec, "https://backend-a.test/v1", "cred-a")
	if err != nil {
		t.Fatalf("NewProviderConfig(backend-a) returned error: %v", err)
	}
	cfgA, err = cfgA.WithProviderProtocol("responses")
	if err != nil {
		t.Fatalf("WithProviderProtocol(backend-a) returned error: %v", err)
	}

	cfgB, err := endpointintent.NewProviderConfig(refB, spec, "https://backend-b.test/v1", "cred-b")
	if err != nil {
		t.Fatalf("NewProviderConfig(backend-b) returned error: %v", err)
	}
	cfgB, err = cfgB.WithProviderProtocol("responses")
	if err != nil {
		t.Fatalf("WithProviderProtocol(backend-b) returned error: %v", err)
	}
	cfgB, err = cfgB.WithModelID("provider-model-b")
	if err != nil {
		t.Fatalf("WithModelID(backend-b) returned error: %v", err)
	}

	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfgA, cfgB}, refA)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return name, endpoint
}
