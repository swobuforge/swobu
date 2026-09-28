package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	store "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

type fallbackEvidenceTransport struct{ calls int }

func (t *fallbackEvidenceTransport) Send(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
	t.calls++
	if target.TargetID == "rejected" {
		return nil, provider.AttemptRejectedBeforeExecution(provider.Rejected(errors.New("fixture rejected")))
	}
	raw := []byte(`{"id":"provider-response","model":"model","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`)
	return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, raw, carrier.Meta{})}, nil
}

type fallbackEvidenceRuntime struct {
	codecresolver.RuntimeCodecResolver
	transport *fallbackEvidenceTransport
}

func (r fallbackEvidenceRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	return provider.Backend{Target: target, Codec: protocolcodec.Codec{Protocol: target.ProtocolKind}, Transport: provider.BindTransport(target, r.transport.Send)}, nil
}

func fallbackEvidenceWorkspace(t *testing.T) routing.Workspace {
	t.Helper()
	slug, err := routing.ParseWorkspaceSlug("cache")
	if err != nil {
		t.Fatal(err)
	}
	name, err := routing.ParseRouteName("fallback")
	if err != nil {
		t.Fatal(err)
	}
	model, err := routing.ParseUpstreamModel("model")
	if err != nil {
		t.Fatal(err)
	}
	providerID, err := routing.ParseProvider("custom", func(raw string) bool { return raw == "custom" })
	if err != nil {
		t.Fatal(err)
	}
	protocol, err := routing.ParseProtocol("responses", providerID, func(routing.Provider, string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	var tiers []routing.Tier
	for _, label := range []string{"rejected", "accepted"} {
		id, err := routing.ParseTargetID(label)
		if err != nil {
			t.Fatal(err)
		}
		connection, err := routing.NewCustomConnection(providerID, "https://example.test/v1", nil)
		if err != nil {
			t.Fatal(err)
		}
		target, err := routing.NewTarget(id, model, protocol, connection)
		if err != nil {
			t.Fatal(err)
		}
		tier, err := routing.NewTier([]routing.Target{target})
		if err != nil {
			t.Fatal(err)
		}
		tiers = append(tiers, tier)
	}
	route, err := routing.NewRoute(name, tiers)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.NewWorkspace(slug, name, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func TestFallbackInvocationsTraverseHTTPStoreAndActivity(t *testing.T) {
	workspace := fallbackEvidenceWorkspace(t)
	evidenceStore := store.NewStore(store.Config{})
	transport := &fallbackEvidenceTransport{}
	runtime := fallbackEvidenceRuntime{RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(), transport: transport}
	ingress := exchange.NewIngress(imageIncidentWorkspaceLookup{workspace: workspace}, runtime, exchange.RuntimePoliciesSpec{
		PolicyResolver:    exchange.StaticWorkspacePolicyResolver{Policy: exchange.DefaultWorkspacePolicy()},
		ExecutionEvidence: evidenceStore,
	})
	mux := http.NewServeMux()
	mux.Handle("/c/", localAuthorityTestHandler(NewHandler(ingress)))
	mux.Handle("/_swobu/activity", NewActivityHandler(func(_ *http.Request, scope store.ProjectionScope) (store.ActivityProjection, error) {
		return evidenceStore.ProjectActivity(store.ProjectionInput{Scope: scope}), nil
	}))
	server := httptest.NewServer(mux)
	defer server.Close()
	issueCacheEvidenceRequest(t, server, `{"model":"fallback","input":"hello","stream":false}`)
	if transport.calls != 2 {
		t.Fatalf("provider calls = %d, want rejected then accepted", transport.calls)
	}
	settled := evidenceStore.Settled()
	if len(settled) != 1 || len(settled[0].Invocations) != 2 {
		t.Fatalf("fallback lifecycles = %#v", settled)
	}
	for i, invocation := range settled[0].Invocations {
		if invocation.Started.Ordinal() != uint64(i+1) || invocation.Settled == nil {
			t.Fatalf("fallback invocation %d = %#v", i, invocation)
		}
		terminal, ok := invocation.Settled.(executionevidence.ModelInvocationSettled)
		if !ok {
			t.Fatalf("fallback invocation %d has no model terminal: %#v", i, invocation.Settled)
		}
		if i == 0 && (terminal.Outcome() != execution.Failed || terminal.Occurrence() != execution.DidNotOccur || terminal.Failure() != executionevidence.FailureRejected) {
			t.Fatalf("rejected provider terminal = %#v", terminal)
		}
		if i == 1 && (terminal.Outcome() != execution.Succeeded || terminal.Occurrence() != execution.Occurred || terminal.Failure() != "") {
			t.Fatalf("successful fallback terminal = %#v", terminal)
		}
	}
	activity, err := operatorclient.New(server.Client(), server.URL).Activity(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(activity.Exchanges) != 1 || activity.Exchanges[0].AttemptCount != 2 || activity.Exchanges[0].State != "succeeded" ||
		activity.Exchanges[0].Provider != "custom" || strings.TrimSpace(activity.Exchanges[0].Model) == "" {
		t.Fatalf("operator fallback Activity = %#v", activity.Exchanges)
	}
}
