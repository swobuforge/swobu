package exchange

import (
	"context"
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/routing"
)

func requestpathTarget(t *testing.T, id string) routing.Target {
	t.Helper()
	targetID, _ := routing.ParseTargetID(id)
	model, _ := routing.ParseUpstreamModel("upstream-" + id)
	connection, _ := routing.NewCustomConnection("https://example.test/v1", nil)
	protocol, _ := routing.ParseProtocol("responses", routing.ProviderCustom, func(routing.Provider, string) bool { return true })
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

func TestRequestPathNeverAttemptsTargetFromAnotherRoute(t *testing.T) {
	workspace := requestpathWorkspace(t)
	var refs []string
	ingress := RequestIngress{runner: withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		refs = append(refs, req.Target.BackendRef)
		return carrier.NewCarrierDocument(carrier.StageProviderIngressIn, req.Target.ProtocolKind, "application/json", nil, []byte(`{"id":"resp","model":"m","output_text":"ok"}`), carrier.Meta{}), nil
	})}
	_, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, RequestInput{ExchangeID: "route-a", Request: NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"a","input":"hi"}`)), ClientFamily: canonical.ClientFamilyResponses, ResponseFraming: delivery.FramingSSE})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0] != "target-a" {
		t.Fatalf("attempted targets=%v", refs)
	}
}
func TestRequestPathUnknownModelAttemptsDefaultRoute(t *testing.T) {
	workspace := requestpathWorkspace(t)
	var refs []string
	ingress := RequestIngress{runner: withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		refs = append(refs, req.Target.BackendRef)
		return carrier.NewCarrierDocument(carrier.StageProviderIngressIn, req.Target.ProtocolKind, "application/json", nil, []byte(`{"id":"resp","model":"m","output_text":"ok"}`), carrier.Meta{}), nil
	})}
	_, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, RequestInput{ExchangeID: "unknown", Request: NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"missing","input":"hi"}`)), ClientFamily: canonical.ClientFamilyResponses, ResponseFraming: delivery.FramingSSE})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0] != "target-a" {
		t.Fatalf("attempted targets=%v", refs)
	}
}
