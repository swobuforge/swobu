package exchange

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestSemanticHandoffKeepsLifecycleMetadataInsideExchangeOnUnavailableFailure(t *testing.T) {
	want := provider.Unavailable(errors.New("provider stream read timed out"))
	stream := &failAfterEventsStream{events: []canonical.Event{
		{Kind: canonical.EventEnvelopeStart},
		{Kind: canonical.EventResponseIdentity},
	}, err: want}

	got, err := prefetchSemanticHandoff(context.Background(), stream)
	if got != nil || !errors.Is(err, want) {
		t.Fatalf("prefetch = (%T, %v), want unavailable failure before handoff", got, err)
	}
}

func TestSemanticHandoffReplaysLifecyclePrefixAtFirstSubstantiveEvent(t *testing.T) {
	stream := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart},
		{Kind: canonical.EventResponseIdentity},
		{Kind: canonical.EventItemStart},
		{Kind: canonical.EventTextDelta},
	})

	prefetched, err := prefetchSemanticHandoff(context.Background(), stream)
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

func TestExchangeFallsBackWhenUnavailablePrecedesSemanticHandoff(t *testing.T) {
	runner, workspace, calls := semanticHandoffFallbackFixture(t, 2)
	out, err := runExchange(context.Background(), runner, "semantic_fallback", "unknown", canonical.ClientFamilyResponses, delivery.StreamingDelivery(delivery.FramingSSE), testDecodedRequest(testCanonicalRequest("a")), nil, workspace, nil, canonical.NormalizedPathResponses)
	if err != nil {
		t.Fatal(err)
	}
	if out.Target.TargetID != "semantic-b" || !reflect.DeepEqual(*calls, []string{"semantic-a", "semantic-b"}) {
		t.Fatalf("target/calls = %q %#v, want fallback target B", out.Target.TargetID, *calls)
	}
	if _, err := io.ReadAll(ClientTransportForTest(out.Response).Body); err != nil {
		t.Fatal(err)
	}
}

func TestExchangeDoesNotFallBackAfterSemanticHandoff(t *testing.T) {
	runner, workspace, calls := semanticHandoffFallbackFixture(t, 3)
	out, err := runExchange(context.Background(), runner, "semantic_committed", "unknown", canonical.ClientFamilyResponses, delivery.StreamingDelivery(delivery.FramingSSE), testDecodedRequest(testCanonicalRequest("a")), nil, workspace, nil, canonical.NormalizedPathResponses)
	if err != nil {
		t.Fatal(err)
	}
	if out.Target.TargetID != "semantic-a" {
		t.Fatalf("target = %q, want committed target A", out.Target.TargetID)
	}
	raw, err := io.ReadAll(ClientTransportForTest(out.Response).Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("expected target A terminal stream projection")
	}
	if !reflect.DeepEqual(*calls, []string{"semantic-a"}) {
		t.Fatalf("calls = %#v, fallback must remain closed after item.start", *calls)
	}
}

type semanticHandoffFallbackRuntime struct {
	testRuntimeResolver
	failAfter int
	calls     *[]string
}

func (r semanticHandoffFallbackRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	codec := semanticHandoffFallbackCodec{targetID: target.TargetID, failAfter: r.failAfter}
	transport := provider.BindTransport(target, func(_ context.Context, selected provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
		*r.calls = append(*r.calls, selected.TargetID)
		return provider.DocumentIngress{Document: carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{})}, nil
	})
	return provider.Backend{Target: target, Codec: codec, Transport: transport}, nil
}

type semanticHandoffFallbackCodec struct {
	targetID  string
	failAfter int
}

func (semanticHandoffFallbackCodec) Encode(provider.Request) (carrier.Document, []compat.Change, error) {
	return carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{}), nil, nil
}

func (c semanticHandoffFallbackCodec) Decode(_ context.Context, request provider.Request, _ provider.Ingress) (provider.DecodedResponse, error) {
	stream := stubResponseEventReader(request.ExchangeID)
	if c.targetID == "semantic-a" {
		stream = &failAfterNStream{upstream: stream, remaining: c.failAfter, err: provider.Unavailable(errors.New("primary unavailable"))}
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

func semanticHandoffFallbackFixture(t *testing.T, failAfter int) (Runner, routing.Workspace, *[]string) {
	t.Helper()
	slug, _ := routing.ParseWorkspaceSlug("dev")
	routeName, _ := routing.ParseRouteName("a")
	tier, err := routing.NewTier([]routing.Target{requestpathTarget(t, "semantic-a"), requestpathTarget(t, "semantic-b")})
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
	runner.Runtime = semanticHandoffFallbackRuntime{failAfter: failAfter, calls: calls}
	return runner, workspace, calls
}
