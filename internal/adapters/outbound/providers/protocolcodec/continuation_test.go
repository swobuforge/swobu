package protocolcodec

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/replay"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
)

type cancellationBody struct{ closed chan struct{} }

func (b *cancellationBody) Read([]byte) (int, error) {
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *cancellationBody) Close() error {
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return nil
}

func TestDecodePreservesExchangeIdentity(t *testing.T) {
	document := carrier.NewDocument(
		protocolkind.Responses,
		"application/json",
		nil,
		[]byte(`{"id":"provider_response","model":"m","output_text":"ok"}`),
		carrier.Meta{},
	)
	decoded, err := (Codec{ProviderID: "openai", Protocol: protocolkind.Responses}).Decode(context.Background(), provider.Request{ExchangeID: "ex_decode_identity"}, provider.DocumentIngress{Document: document})
	if err != nil {
		t.Fatal(err)
	}
	stream := decoded.Stream
	defer stream.Close(context.Background())
	for {
		event, nextErr := stream.Next(context.Background())
		if nextErr == io.EOF {
			t.Fatal("provider response ended without a canonical event")
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		if event.ExchangeID != "ex_decode_identity" {
			t.Fatalf("canonical event exchange id = %q, want invocation exchange id", event.ExchangeID)
		}
		break
	}
}

func TestDecodeStreamRetainsInvocationCancellation(t *testing.T) {
	body := &cancellationBody{closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	decoded, err := (Codec{ProviderID: "openai", Protocol: protocolkind.Responses}).Decode(ctx, provider.Request{ExchangeID: "ex_decode_cancel"}, provider.StreamIngress{Stream: carrier.ByteStream{
		MediaType: "text/event-stream",
		Body:      body,
	}})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, nextErr := decoded.Stream.Next(ctx)
		result <- nextErr
	}()
	cancel()
	select {
	case nextErr := <-result:
		if !errors.Is(nextErr, context.Canceled) {
			t.Fatalf("canonical provider reader error = %v, want invocation cancellation", nextErr)
		}
	case <-time.After(time.Second):
		t.Fatal("provider decode stream detached from invocation cancellation")
	}
}

func continuationTestTarget(t *testing.T, protocol protocolkind.ProtocolKind) provider.TargetSnapshot {
	t.Helper()
	target := provider.NewTargetSnapshot("target-test", "test", "https://example.test", "account-a", protocol, "")
	target.Model = "m"
	return target
}

func continuationPrepared(t *testing.T, target provider.TargetSnapshot, withResponsesRefinement bool) replay.Prepared {
	semantic := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{
		canonicaltest.Message(t, canonical.MessageRoleUser, "turn one"),
		canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer one"),
		canonicaltest.Message(t, canonical.MessageRoleUser, "turn two"),
	}})
	var previous *canonical.ResponseRef
	if withResponsesRefinement {
		previous = &canonical.ResponseRef{SwobuID: "swobu_previous", Responses: &canonical.ResponsesNativeRef{
			ProviderResponseID: "provider_previous", TargetID: target.TargetID, TargetVersion: target.TargetVersion,
		}}
	}
	delta := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "turn two")}, PreviousResponse: previous})
	return replay.Prepared{Semantic: semantic, Delta: delta}
}

func TestStatelessBackendCodecsReceiveFullSemanticReplay(t *testing.T) {
	for _, tc := range []struct {
		name       string
		providerID string
		protocol   protocolkind.ProtocolKind
	}{
		{name: "chat_completions", providerID: "openai", protocol: protocolkind.ChatCompletions},
		{name: "messages", providerID: "anthropic", protocol: protocolkind.Messages},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := continuationTestTarget(t, tc.protocol)
			request := provider.Request{Canonical: continuationPrepared(t, target, false).PreferredForTarget(target), Delivery: delivery.BufferedDelivery()}
			codec := Codec{ProviderID: tc.providerID, Protocol: tc.protocol}
			if tc.protocol == protocolkind.ChatCompletions {
				codec.Options.ChatCompletionsTokenField = chatcompletions.MaxOutputTokensFieldCompletion
			}
			document, _, err := codec.Encode(request)
			if err != nil {
				t.Fatal(err)
			}
			raw := string(document.RawBytes())
			if _, ok := request.Canonical.PreviousResponse(); ok || !strings.Contains(raw, "turn one") || !strings.Contains(raw, "answer one") || !strings.Contains(raw, "turn two") {
				t.Fatalf("stateless replay lost semantic history: %s", raw)
			}
		})
	}
}

func TestResponsesBackendWithMatchingCanonicalRefinementSendsDeltaAndPreviousResponseID(t *testing.T) {
	target := continuationTestTarget(t, protocolkind.Responses)
	request := provider.Request{Canonical: continuationPrepared(t, target, true).PreferredForTarget(target), Delivery: delivery.BufferedDelivery()}
	document, _, err := (Codec{ProviderID: "openai", Protocol: protocolkind.Responses}).Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(document.RawBytes())
	if !strings.Contains(raw, `"previous_response_id":"provider_previous"`) || !strings.Contains(raw, "turn two") {
		t.Fatalf("native continuation was not lowered: %s", raw)
	}
	if strings.Contains(raw, "turn one") || strings.Contains(raw, "answer one") {
		t.Fatalf("native continuation redundantly sent semantic history: %s", raw)
	}
}
