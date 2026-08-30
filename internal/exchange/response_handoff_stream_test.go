package exchange

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestResponseHandoffKeepsEnvelopeStartInsideExchangeOnUnavailableFailure(t *testing.T) {
	want := provider.Unavailable(errors.New("provider stream read timed out"))
	stream := &failAfterEventsStream{events: []canonical.Event{
		{Kind: canonical.EventEnvelopeStart},
	}, err: want}

	got, err := prefetchResponseHandoff(context.Background(), stream, true)
	if got != nil || !errors.Is(err, want) {
		t.Fatalf("prefetch = (%T, %v), want unavailable failure before handoff", got, err)
	}
}

func TestResponseHandoffReplaysOpeningPrefixAtValidatedIdentity(t *testing.T) {
	stream := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart},
		{Kind: canonical.EventResponseIdentity},
		{Kind: canonical.EventItemStart},
		{Kind: canonical.EventTextDelta},
	})

	prefetched, err := prefetchResponseHandoff(context.Background(), stream, true)
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []canonical.EventKind{
		canonical.EventEnvelopeStart,
		canonical.EventResponseIdentity,
		canonical.EventItemStart,
		canonical.EventTextDelta,
	} {
		event, nextErr := prefetched.Next(context.Background())
		if nextErr != nil || event.Kind != want {
			t.Fatalf("event %d = (%s, %v), want %s", index, event.Kind, nextErr, want)
		}
	}
}

func TestBufferedResponseHandoffKeepsIdentityInsideExchangeUntilOutput(t *testing.T) {
	want := provider.Unavailable(errors.New("provider stream failed after response identity"))
	stream := &failAfterEventsStream{events: []canonical.Event{
		{Kind: canonical.EventEnvelopeStart},
		{Kind: canonical.EventResponseIdentity},
	}, err: want}

	got, err := prefetchResponseHandoff(context.Background(), stream, false)
	if got != nil || !errors.Is(err, want) {
		t.Fatalf("prefetch = (%T, %v), want unavailable failure before buffered handoff", got, err)
	}
}

func TestExchangeFallsBackWhenUnavailablePrecedesResponseIdentity(t *testing.T) {
	runner, workspace, calls := responseHandoffFallbackFixture(t, 1)
	out, err := runExchange(context.Background(), runner, "response_fallback", "unknown", canonical.ClientFamilyResponses, delivery.StreamingDelivery(delivery.FramingSSE), testDecodedRequest(testCanonicalRequest("a")), nil, workspace, nil, canonical.NormalizedPathResponses)
	if err != nil {
		t.Fatal(err)
	}
	if out.Target.TargetID != "response-b" || !reflect.DeepEqual(*calls, []string{"response-a", "response-b"}) {
		t.Fatalf("target/calls = %q %#v, want fallback target B", out.Target.TargetID, *calls)
	}
	if _, err := io.ReadAll(ClientTransportForTest(out.Response).Body); err != nil {
		t.Fatal(err)
	}
}

func TestExchangeDoesNotFallBackAfterResponseIdentity(t *testing.T) {
	runner, workspace, calls := responseHandoffFallbackFixture(t, 2)
	out, err := runExchange(context.Background(), runner, "response_committed", "unknown", canonical.ClientFamilyMessages, delivery.StreamingDelivery(delivery.FramingSSE), testDecodedRequest(testCanonicalRequest("a")), nil, workspace, nil, canonical.NormalizedPathMessages)
	if err != nil {
		t.Fatal(err)
	}
	if out.Target.TargetID != "response-a" {
		t.Fatalf("target = %q, want committed target A", out.Target.TargetID)
	}
	raw, readErr := io.ReadAll(ClientTransportForTest(out.Response).Body)
	if readErr != nil {
		t.Fatalf("committed Messages stream read error = %v, want protocol-native terminal frame", readErr)
	}
	for _, required := range []string{"event: message_start", "event: error", `"code":"provider_stream_decode_failed"`} {
		if !strings.Contains(string(raw), required) {
			t.Fatalf("committed Messages stream lacks %q: %s", required, raw)
		}
	}
	for _, forbidden := range []string{"event: message_stop", `"type":"message_stop"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("failed committed Messages stream contains success terminal %q: %s", forbidden, raw)
		}
	}
	if !reflect.DeepEqual(*calls, []string{"response-a"}) {
		t.Fatalf("calls = %#v, fallback must remain closed after response identity", *calls)
	}
}

func TestExchangeFallsBackWhenHostedSearchFailsAfterResponseIdentity(t *testing.T) {
	runner, workspace, calls := responseHandoffFallbackFixture(t, 2)
	request := testCanonicalRequest("a")
	tools, err := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	if err != nil {
		t.Fatal(err)
	}
	declarations, err := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	request = request.WithItems(append([]canonical.CanonicalItem{declarations}, request.Items()...))
	searchKey := canonical.WebSearchToolKey()
	request = canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:      request.ModelField(),
		Items:      request.Items(),
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &searchKey)),
	})

	out, err := runExchange(context.Background(), runner, "response_committed", "unknown", canonical.ClientFamilyMessages, delivery.StreamingDelivery(delivery.FramingSSE), testDecodedRequest(request), nil, workspace, nil, canonical.NormalizedPathMessages)
	if err != nil {
		t.Fatal(err)
	}
	if out.Target.TargetID != "response-b" {
		t.Fatalf("target = %q, want fallback target B", out.Target.TargetID)
	}
	if !reflect.DeepEqual(*calls, []string{"response-a", "response-b"}) {
		t.Fatalf("calls = %#v, want hosted-search failure before handoff then fallback", *calls)
	}
	if _, err := io.ReadAll(ClientTransportForTest(out.Response).Body); err != nil {
		t.Fatal(err)
	}
}

func TestOfferedHostedSearchDoesNotDelayOrdinaryIdentityHandoff(t *testing.T) {
	tools, err := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	if err != nil {
		t.Fatal(err)
	}
	declarations, err := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Items:      []canonical.CanonicalItem{declarations},
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil)),
	})
	delayed, err := delayClientHandoffFor(nil, request)
	if err != nil {
		t.Fatal(err)
	}
	if delayed {
		t.Fatal("optional hosted-search declaration delayed ordinary incremental response")
	}
}

type responseHandoffFallbackRuntime struct {
	RuntimeResolver
	failAfter int
	calls     *[]string
}

func (r responseHandoffFallbackRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	codec := responseHandoffFallbackCodec{targetID: target.TargetID, failAfter: r.failAfter}
	transport := provider.BindTransport(target, func(_ context.Context, selected provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		*r.calls = append(*r.calls, selected.TargetID)
		return provider.DocumentIngress{Document: carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{})}, nil
	})
	return provider.Backend{Target: target, Codec: codec, Transport: transport}, nil
}

type responseHandoffFallbackCodec struct {
	targetID  string
	failAfter int
}

func (responseHandoffFallbackCodec) Encode(provider.Request) (carrier.Document, []compat.Change, error) {
	return carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{}), nil, nil
}

func (c responseHandoffFallbackCodec) Decode(_ context.Context, request provider.Request, _ provider.Ingress) (provider.DecodedResponse, error) {
	stream := stubResponseEventReader(request.ExchangeID)
	if c.targetID == "response-a" {
		failure := provider.Unavailable(errors.New("primary unavailable"))
		if request.Canonical.ToolPolicy().Mode == canonical.ToolPolicySpecific {
			failure = provider.Rejected(canonical.NewBackendError("responses", http.StatusUnauthorized, "authentication_error", ""))
		}
		stream = &failAfterNStream{upstream: stream, remaining: c.failAfter, err: failure}
	}
	return provider.DecodedResponse{Stream: stream}, nil
}

type failAfterNStream struct {
	upstream  canonical.ResponseStream
	remaining int
	err       error
}

func (s *failAfterNStream) Next(ctx context.Context) (canonical.Event, error) {
	if s.remaining == 0 {
		return canonical.Event{}, s.err
	}
	s.remaining--
	return s.upstream.Next(ctx)
}

func (s *failAfterNStream) Close(ctx context.Context) error { return s.upstream.Close(ctx) }

func responseHandoffFallbackFixture(t *testing.T, failAfter int) (Runner, routing.Workspace, *[]string) {
	t.Helper()
	slug, _ := routing.ParseWorkspaceSlug("dev")
	routeName, _ := routing.ParseRouteName("a")
	tier, err := routing.NewTier([]routing.Target{requestpathTarget(t, "response-a"), requestpathTarget(t, "response-b")})
	if err != nil {
		t.Fatal(err)
	}
	route, err := routing.NewRoute(routeName, []routing.Tier{tier})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	calls := &[]string{}
	runner := withRuntime(nil)
	runner.Runtime = responseHandoffFallbackRuntime{RuntimeResolver: codecresolver.NewRuntimeCodecResolver(), failAfter: failAfter, calls: calls}
	return runner, workspace, calls
}
