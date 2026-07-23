package protocolcodec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
)

func TestEncodeSelectsFullChatHistoryAndNativeResponsesDelta(t *testing.T) {
	full := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "first turn"),
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "first answer"),
			canonicaltest.Message(t, canonical.MessageRoleUser, "second turn"),
		},
	})
	chat, _, err := (Codec{Protocol: protocolkind.ChatCompletions}).Encode(provider.Request{Canonical: full, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if raw := string(chat.RawBytes()); !strings.Contains(raw, "first turn") || !strings.Contains(raw, "first answer") || !strings.Contains(raw, "second turn") {
		t.Fatalf("chat codec did not preserve full history: %s", raw)
	}

	delta := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "second turn")},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_previous", Responses: &canonical.ResponsesNativeRef{
			ProviderResponseID: "provider_previous", TargetID: "target", TargetVersion: 1,
		}},
	})
	responses, _, err := (Codec{Protocol: protocolkind.Responses}).Encode(provider.Request{Canonical: delta, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if raw := string(responses.RawBytes()); !strings.Contains(raw, `"previous_response_id":"provider_previous"`) || strings.Contains(raw, "first turn") {
		t.Fatalf("responses codec did not preserve native delta selection: %s", raw)
	}
}

func TestChatCompletionsWebSearchUsesProtocolDefault(t *testing.T) {
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "search")},
		Tools: canonical.Specify(set),
	})
	document, _, err := (Codec{Protocol: protocolkind.ChatCompletions}).Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.RawBytes()), `"web_search_options":{}`) {
		t.Fatalf("protocol default missing from %s", document.RawBytes())
	}
}

func TestChatCompletionsCodecSerializesSharedTypedLowering(t *testing.T) {
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	request := provider.Request{
		ExchangeID: "exchange-shared-lowering",
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("model"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "search")},
			Tools: canonical.Specify(set),
		}),
		Delivery: delivery.StreamingDelivery(delivery.FramingSSE),
	}
	typed, typedDecisions, err := LowerChatCompletionsRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	fromTyped, err := chatcompletions.EncodeProviderRequestDocument(typed)
	if err != nil {
		t.Fatal(err)
	}
	fromCodec, codecDecisions, err := (Codec{Protocol: protocolkind.ChatCompletions}).Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fromTyped.RawBytes(), fromCodec.RawBytes()) {
		t.Fatalf("shared typed lowering = %s, codec = %s", fromTyped.RawBytes(), fromCodec.RawBytes())
	}
	if !reflect.DeepEqual(typedDecisions, codecDecisions) {
		t.Fatalf("shared decisions = %#v, codec decisions = %#v", typedDecisions, codecDecisions)
	}
}

func TestDecodePreservesExchangeIdentityAndCancellation(t *testing.T) {
	codec := Codec{Protocol: protocolkind.ChatCompletions}
	request := provider.Request{ExchangeID: "exchange-identity", Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model")})}
	decoded, err := codec.Decode(context.Background(), request, provider.DocumentIngress{Document: carrier.NewDocument(
		protocolkind.ChatCompletions, "application/json", nil,
		[]byte(`{"id":"provider-response","model":"model","choices":[{"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}]}`), carrier.Meta{},
	)})
	if err != nil {
		t.Fatal(err)
	}
	event, err := decoded.Stream.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if event.ExchangeID != request.ExchangeID {
		t.Fatalf("exchange id = %q", event.ExchangeID)
	}

	body := &blockingReadCloser{closed: make(chan struct{})}
	streamed, err := codec.Decode(context.Background(), request, provider.StreamIngress{Stream: carrier.ByteStream{
		Header: http.Header{"Content-Type": {"text/event-stream"}}, MediaType: "text/event-stream", Body: body,
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, nextErr := streamed.Stream.Next(ctx)
		result <- nextErr
	}()
	cancel()
	select {
	case nextErr := <-result:
		if !errors.Is(nextErr, context.Canceled) {
			t.Fatalf("Next error = %v", nextErr)
		}
	case <-time.After(time.Second):
		t.Fatal("protocol codec stream ignored cancellation")
	}
}

type blockingReadCloser struct{ closed chan struct{} }

func (b *blockingReadCloser) Read([]byte) (int, error) {
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *blockingReadCloser) Close() error {
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return nil
}
