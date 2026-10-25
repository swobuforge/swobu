package bootstrap

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	credentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/chatgpt"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	evidence "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/connectionfixture"
)

type refreshJourneyTransport func(*http.Request) (*http.Response, error)

func (f refreshJourneyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type refreshJourneyRuntime struct {
	codecresolver.RuntimeCodecResolver
	provider.BackendResolver
}
type refreshJourneyEvidence struct{ events []evidence.Event }

func (s *refreshJourneyEvidence) AppendExecutionEvidence(e evidence.Event) error {
	s.events = append(s.events, e)
	return nil
}

func TestConnectionRefreshRequestJourneyRetainsBothInvocationRevisions(t *testing.T) {
	t.Setenv("SWOBU_HOME", t.TempDir())
	calls, refreshes := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			refreshes++
			if err := r.ParseForm(); err != nil || r.Form.Get("refresh_token") != "synthetic-refresh" {
				t.Error("wrong refresh exchange")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"synthetic-new","refresh_token":"synthetic-next","expires_in":3600}`)
			return
		}
		calls++
		if calls == 1 {
			if r.Header.Get("Authorization") != "Bearer synthetic-old" {
				t.Error("first request lost old slot")
			}
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"message":"expired"}}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-new" {
			t.Error("retry lost refreshed slot")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fixture\",\"model\":\"fixture\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg_fixture\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\",\"annotations\":[]}]}]}}\n\n")
	}))
	defer server.Close()
	destination, _ := url.Parse(server.URL)
	client := &http.Client{Transport: refreshJourneyTransport(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		copied := *r.URL
		clone.URL = &copied
		clone.URL.Scheme = destination.Scheme
		clone.URL.Host = destination.Host
		return server.Client().Transport.RoundTrip(clone)
	})}
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	registry, err := connectionfixture.Open(path, profile.SupportsSpec)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.Parse("cn_journey")
	bundle, err := credentials.EncodeTokenBundle(credentials.TokenBundle{AccessToken: "synthetic-old", RefreshToken: "synthetic-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := credentials.StoreConnectionCredential(id.String(), bundle, credentials.CredentialWritePolicyFile)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := routing.NewStandardConnection("chatgpt", server.URL, ref)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewModelSpec(upstream)
	if err != nil {
		t.Fatal(err)
	}
	value, err := connections.New(id, "Journey", spec, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(value, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
	target, err := routing.FinalizeTarget(routing.TargetDraft{ID: "target", Model: "fixture", Protocol: "responses_stream", ConnectionID: id.String()}, profile.RoutingConstructionFacts())
	if err != nil {
		t.Fatal(err)
	}
	tier, err := routing.NewTier([]routing.Target{target})
	if err != nil {
		t.Fatal(err)
	}
	routeName, _ := routing.ParseRouteName("primary")
	route, err := routing.NewRoute(routeName, []routing.Tier{tier})
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("journey")
	workspace, err := routing.NewWorkspace(slug, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	capture := &refreshJourneyEvidence{}
	ingress := exchange.NewIngress(nil, refreshJourneyRuntime{codecresolver.NewRuntimeCodecResolver(), chatgpt.NewExecutor(client, credentials.NewResolver())}, exchange.RuntimePoliciesSpec{
		ModelConnection: registry.Get, ExecutionEvidence: capture,
		PublishRefreshedCredential: func(ctx context.Context, target provider.TargetSnapshot, material string) error {
			return publishRefreshedConnection(ctx, registry, target, material, credentials.CredentialWritePolicyFile)
		},
	})
	out, err := ingress.HandleRequestWithWorkspace(t.Context(), workspace, exchange.RequestInput{AccessID: accessidentity.Local, ClientFamily: clientfamily.Codex, ClientRequestID: "refresh-journey", Request: exchange.NewTransportRequest("POST", "/responses", nil, []byte(`{"model":"primary","input":"hello","stream":false}`)), Operation: canonical.ClientOperation{Family: canonical.ClientFamilyResponses, NormalizedPath: canonical.NormalizedPathResponses}})
	if err != nil {
		t.Fatal(err)
	}
	response, ok := out.Response.(exchange.BufferedResponse)
	if !ok {
		t.Fatalf("response %T", out.Response)
	}
	body, err := io.ReadAll(response.Response.Body)
	if err != nil || !strings.Contains(string(body), "hello") {
		t.Fatal("retry did not complete", err)
	}
	_ = response.Response.Body.Close()
	if out.FinalizeExecutionEvidence != nil {
		out.FinalizeExecutionEvidence(delivery.Result{Kind: delivery.Succeeded, HTTPStatus: 200})
	}
	if calls != 2 || refreshes != 1 {
		t.Fatalf("requests=%d refreshes=%d", calls, refreshes)
	}
	var starts []evidence.ModelInvocationStarted
	var settlements []evidence.ModelInvocationSettled
	for _, event := range capture.events {
		switch e := event.(type) {
		case evidence.ModelInvocationStarted:
			starts = append(starts, e)
		case evidence.ModelInvocationSettled:
			settlements = append(settlements, e)
		}
	}
	if len(starts) != 2 || len(settlements) != 2 {
		t.Fatalf("starts=%d settlements=%d", len(starts), len(settlements))
	}
	for index, start := range starts {
		got, revision, bound := start.Model().ConnectionIdentity()
		if !bound || got != id || revision != uint64(index+1) || start.Model().Version() != target.Version() {
			t.Fatal("invocation lost composite identity")
		}
	}
	if starts[0].ID() == starts[1].ID() || settlements[0].Occurrence() != execution.DidNotOccur || settlements[1].Outcome() != execution.Succeeded {
		t.Fatal("invocation settlement conflated attempts")
	}
	// A second target shares the Connection, not the first target's captured
	// snapshot. Its next execution must resolve the published revision without
	// changing either target's identity or rewriting earlier invocation evidence.
	second, err := routing.FinalizeTarget(routing.TargetDraft{ID: "second", Model: "fixture", Protocol: "responses_stream", ConnectionID: id.String()}, profile.RoutingConstructionFacts())
	if err != nil {
		t.Fatal(err)
	}
	secondTier, err := routing.NewTier([]routing.Target{second})
	if err != nil {
		t.Fatal(err)
	}
	secondRoute, err := routing.NewRoute(routeName, []routing.Tier{secondTier})
	if err != nil {
		t.Fatal(err)
	}
	secondWorkspace, err := routing.NewWorkspace(slug, []routing.Route{secondRoute})
	if err != nil {
		t.Fatal(err)
	}
	secondOut, err := ingress.HandleRequestWithWorkspace(t.Context(), secondWorkspace, exchange.RequestInput{AccessID: accessidentity.Local, ClientFamily: clientfamily.Codex, ClientRequestID: "shared-connection-journey", Request: exchange.NewTransportRequest("POST", "/responses", nil, []byte(`{"model":"primary","input":"hello","stream":false}`)), Operation: canonical.ClientOperation{Family: canonical.ClientFamilyResponses, NormalizedPath: canonical.NormalizedPathResponses}})
	if err != nil {
		t.Fatal(err)
	}
	secondResponse, ok := secondOut.Response.(exchange.BufferedResponse)
	if !ok {
		t.Fatalf("second response %T", secondOut.Response)
	}
	secondBody, err := io.ReadAll(secondResponse.Response.Body)
	_ = secondResponse.Response.Body.Close()
	if err != nil || !strings.Contains(string(secondBody), "hello") {
		t.Fatal("shared target did not complete", err)
	}
	if secondOut.FinalizeExecutionEvidence != nil {
		secondOut.FinalizeExecutionEvidence(delivery.Result{Kind: delivery.Succeeded, HTTPStatus: 200})
	}
	var sharedStarts []evidence.ModelInvocationStarted
	for _, event := range capture.events {
		if start, ok := event.(evidence.ModelInvocationStarted); ok {
			sharedStarts = append(sharedStarts, start)
		}
	}
	if len(sharedStarts) != 3 || calls != 3 || refreshes != 1 {
		t.Fatalf("shared starts=%d calls=%d refreshes=%d", len(sharedStarts), calls, refreshes)
	}
	got, revision, bound := sharedStarts[2].Model().ConnectionIdentity()
	if !bound || got != id || revision != 2 || sharedStarts[2].Model().Target() != second.ID() || sharedStarts[2].Model().Version() != second.Version() || sharedStarts[2].ID() == starts[1].ID() {
		t.Fatal("second target lost independently captured shared Connection revision")
	}
	old, err := credentials.ResolveConnectionStoredBundle(id.String(), ref)
	if err != nil || old != bundle {
		t.Fatal("request journey mutated captured slot", err)
	}
	reopened, err := connectionfixture.Open(path, profile.SupportsSpec)
	if err != nil {
		t.Fatal(err)
	}
	current, err := reopened.Get(id)
	if err != nil || current.Revision().String() != "2" {
		t.Fatal("refresh revision not durable", err)
	}
}
