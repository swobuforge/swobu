package messages

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestMessagesReasoningBudgetKeepsMaxTokensValid(t *testing.T) {
	compute, _ := canonical.NewBudgetReasoningCompute(2048)
	reasoning, _ := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(compute)})
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("hi")})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("claude"), Items: []canonical.CanonicalItem{message}, Reasoning: reasoning})
	document, err := EncodeCarrierWithChanges(request, testAttemptToolNames(request), delivery.BufferedDelivery(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["max_tokens"] != float64(2304) {
		t.Fatalf("max_tokens = %#v, want 2304", payload["max_tokens"])
	}
	max := 2048
	controls, _ := canonical.NewGenerationControls(canonical.GenerationControlsParams{MaxOutputTokens: &max})
	request = canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("claude"), Items: []canonical.CanonicalItem{message}, Controls: controls, Reasoning: reasoning})
	if _, err := EncodeCarrierWithChanges(request, testAttemptToolNames(request), delivery.BufferedDelivery(), nil, ""); err == nil {
		t.Fatal("max_tokens equal to budget_tokens was accepted")
	}
}

func TestMessagesDisclosureAloneDoesNotActivateOrRejectReasoning(t *testing.T) {
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("hi")})
	for _, disclosure := range []canonical.ReasoningDisclosure{canonical.ReasoningDisclosureNone, canonical.ReasoningDisclosureSummary} {
		reasoning, _ := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Disclosure: canonical.Specify(disclosure)})
		request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("claude"), Items: []canonical.CanonicalItem{message}, Reasoning: reasoning})
		document, err := EncodeCarrierWithChanges(request, testAttemptToolNames(request), delivery.BufferedDelivery(), nil, "")
		if err != nil {
			t.Fatalf("disclosure %q: %v", disclosure, err)
		}
		if strings.Contains(string(document.RawBytes()), `"thinking"`) {
			t.Fatalf("disclosure %q activated backend thinking: %s", disclosure, document.RawBytes())
		}
	}
}

func TestMessagesReasoningRequestRoundTrip(t *testing.T) {
	raw := []byte(`{"model":"claude","max_tokens":512,"messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":128,"display":"omitted"}}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	compute, ok := decoded.Request.Request.Reasoning().ComputeField().Get()
	if !ok || compute.Kind() != canonical.ReasoningBudget {
		t.Fatalf("compute = %#v", compute)
	}
	document, err := EncodeCarrierWithChanges(decoded.Request.Request, nil, delivery.BufferedDelivery(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.RawBytes()), `"display":"omitted"`) || !strings.Contains(string(document.RawBytes()), `"budget_tokens":128`) {
		t.Fatalf("encoded = %s", document.RawBytes())
	}
}

func TestMessagesOpaqueThinkingReplaysAndDisclosureProjectsWithoutMutation(t *testing.T) {
	rawBlock := []byte(`{"type":"thinking","thinking":"brief","signature":"sig"}`)
	opaque, _ := canonical.NewMessagesOpaqueThinking(rawBlock)
	part, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "brief")
	item, _ := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("claude"), Items: []canonical.CanonicalItem{item}})
	document, err := EncodeCarrierWithChanges(request, testAttemptToolNames(request), delivery.BufferedDelivery(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.RawBytes()), `"signature":"sig"`) {
		t.Fatalf("provider replay = %s", document.RawBytes())
	}

	disclosure, _ := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Disclosure: canonical.Specify(canonical.ReasoningDisclosureNone)})
	clientRequest := canonical.NewCanonicalRequest(canonical.RequestParams{Reasoning: disclosure})
	response, _ := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: "resp"}, "claude", []canonical.CanonicalItem{item}, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(clientRequest, response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded.Document.RawBytes()), `"thinking":""`) || !strings.Contains(string(encoded.Document.RawBytes()), `"signature":"sig"`) {
		t.Fatalf("client projection = %s", encoded.Document.RawBytes())
	}
	restored, _ := item.Reasoning()
	block, _ := restored.Opaque().Messages()
	if string(block) != string(rawBlock) {
		t.Fatal("client disclosure mutated checkpoint truth")
	}
}

func TestMessagesBufferedAndStreamingReasoningAreAtomic(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("claude")})
	bufferedRaw := []byte(`{"id":"msg","model":"claude","content":[{"type":"thinking","thinking":"brief","signature":"sig"}],"stop_reason":"end_turn"}`)
	buffered, err := decodeResponseBuffered(context.Background(), request, testAttemptToolNames(request), bufferedRaw, "ex", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertAtomicReasoning(t, buffered)

	streamRaw := strings.Join([]string{
		`event: message_start\ndata: {"type":"message_start","message":{"id":"msg","model":"claude"}}\n`,
		`event: content_block_start\ndata: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}\n`,
		`event: content_block_delta\ndata: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"brief"}}\n`,
		`event: content_block_delta\ndata: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}\n`,
		`event: content_block_stop\ndata: {"type":"content_block_stop","index":0}\n`,
		`event: message_delta\ndata: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}\n`,
		`event: message_stop\ndata: {"type":"message_stop"}\n`,
	}, "\n")
	streamRaw = strings.ReplaceAll(streamRaw, `\n`, "\n")
	stream := decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{Body: ioNopCloser{strings.NewReader(streamRaw)}}, "ex", nil)
	assertAtomicReasoning(t, stream)
}

func TestMessagesThinkingDefaultsToTrace(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("claude")})
	raw := []byte(`{"id":"msg","model":"claude","content":[{"type":"thinking","thinking":"private","signature":"sig"}],"stop_reason":"end_turn"}`)
	stream, err := decodeResponseBuffered(context.Background(), request, testAttemptToolNames(request), raw, "ex", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := firstReasoningKind(t, stream); got != canonical.ReasoningPartTrace {
		t.Fatalf("reasoning kind = %q, want trace", got)
	}
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, []byte(`{"model":"claude","max_tokens":256,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"history","signature":"sig"}]}]}`), carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	reasoning, _ := decoded.Request.Request.Items()[0].Reasoning()
	if reasoning.Parts()[0].Kind() != canonical.ReasoningPartTrace {
		t.Fatalf("historical thinking kind = %q, want trace", reasoning.Parts()[0].Kind())
	}
}

func firstReasoningKind(t *testing.T, stream canonical.ResponseStream) canonical.ReasoningPartKind {
	t.Helper()
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			t.Fatalf("reasoning item not found: %v", err)
		}
		if event.Kind != canonical.EventItemCompleted {
			continue
		}
		item := event.Payload.(canonical.ItemEvent).Payload.(canonical.ItemCompletedPayload).Item
		if reasoning, ok := item.Reasoning(); ok && len(reasoning.Parts()) > 0 {
			return reasoning.Parts()[0].Kind()
		}
	}
}

func assertAtomicReasoning(t *testing.T, stream canonical.ResponseStream) {
	t.Helper()
	found := false
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			break
		}
		if event.Kind == canonical.EventItemStart || event.Kind == canonical.EventContentStart || event.Kind == canonical.EventTextDelta {
			if payload, ok := event.Payload.(canonical.ItemEvent); ok && payload.Position.Item == 0 {
				t.Fatalf("reasoning emitted progressive event %q", event.Kind)
			}
		}
		if event.Kind == canonical.EventItemCompleted {
			payload := event.Payload.(canonical.ItemEvent).Payload.(canonical.ItemCompletedPayload)
			if reasoning, ok := payload.Item.Reasoning(); ok {
				found = true
				if parts := reasoning.Parts(); len(parts) > 0 && parts[0].Kind() != canonical.ReasoningPartTrace {
					t.Fatalf("unproven Messages thinking kind = %q, want trace", parts[0].Kind())
				}
			}
		}
	}
	if !found {
		t.Fatal("atomic reasoning completion was not emitted")
	}
}

type ioNopCloser struct{ *strings.Reader }

func (ioNopCloser) Close() error { return nil }
