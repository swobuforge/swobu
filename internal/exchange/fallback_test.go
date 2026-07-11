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
)

type providerIngressResolverFunc func(context.Context, ProviderRequest) (ProviderIngress, error)

func (f providerIngressResolverFunc) ResolveProviderIngress(ctx context.Context, req ProviderRequest) (ProviderIngress, error) {
	return f(ctx, req)
}

func TestFallbackBeforeCommit(t *testing.T) {
	ctx := context.Background()
	name, endpoint := testFallbackEndpoint(t)

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

	ingress := RequestIngress{
		providerExec: resolver,
		runner:       withRuntime(Runner{ResolveProviderIngress: resolver}),
		planner: RoutePlanner{
			DeliverySelector: FixedDeliverySelector{},
			Continuation:     canonical.NewContinuationRuntime(nil),
		},
	}

	out, err := ingress.HandleRequestWithEndpoint(ctx, endpoint, RequestInput{
		EndpointName:    name,
		Request:         NewTransportRequest(http.MethodPost, "/responses", http.Header{"Content-Type": []string{"application/json"}}, []byte(`{"model":"m","input":"hello"}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingNone,
		ExchangeID:      "test-exchange",
	})
	if err != nil {
		t.Fatalf("HandleRequestWithEndpoint returned error: %v", err)
	}
	if got := out.Target.BackendRef; got != "backend-b" {
		t.Fatalf("target backend ref = %q, want backend-b", got)
	}
	if !reflect.DeepEqual(calls, []string{"backend-a", "backend-b"}) {
		t.Fatalf("resolver calls = %#v, want backend-a then backend-b", calls)
	}
	raw, err := io.ReadAll(out.Response.Transport.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if !strings.Contains(string(raw), "ok") {
		t.Fatalf("response body = %s, want ok", string(raw))
	}
}

func testFallbackEndpoint(t *testing.T) (endpointintent.EndpointName, endpointintent.Endpoint) {
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
