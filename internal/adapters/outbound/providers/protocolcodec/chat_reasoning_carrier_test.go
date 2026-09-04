package protocolcodec

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestChatReasoningPreludeStreamInsertsBeforeOutputAndResequences(t *testing.T) {
	reasoning := testChatReasoningItem(t, "reasoning")
	start, err := canonical.NewMessageStart(canonical.MessageRoleAssistant)
	if err != nil {
		t.Fatal(err)
	}
	upstream := &testResponseStream{events: []canonical.Event{
		{Kind: canonical.EventEnvelopeStart},
		{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: start}},
		{Kind: canonical.EventTextDelta, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonical.TextDeltaPayload{Text: "answer"}}},
		{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonical.ItemCompletedPayload{Item: testChatReasoningItem(t, "answer")}}},
		{Kind: canonical.EventFinish},
	}}
	stream := newChatReasoningPreludeStream(upstream, reasoning, nil, "exchange-reasoning")

	var events []canonical.Event
	for {
		event, nextErr := stream.Next(context.Background())
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		events = append(events, event)
	}

	if len(events) != 6 {
		t.Fatalf("event count = %d, want 6: %#v", len(events), events)
	}
	if events[1].Kind != canonical.EventItemCompleted {
		t.Fatalf("prelude event = %#v", events[1])
	}
	prelude, ok := events[1].Payload.(canonical.ItemEvent)
	if !ok || prelude.Position.Item != 0 {
		t.Fatalf("prelude payload = %#v", events[1].Payload)
	}
	for index, event := range events {
		if event.ExchangeID != "exchange-reasoning" || event.Seq != int64(index+1) || event.Time.IsZero() {
			t.Fatalf("event %d identity = %#v", index, event)
		}
		if item, ok := event.Payload.(canonical.ItemEvent); ok && index != 1 && item.Position.Item != 1 {
			t.Fatalf("event %d item position = %d, want 1", index, item.Position.Item)
		}
	}
	if err := stream.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !upstream.closed {
		t.Fatal("prelude stream did not close its upstream stream")
	}
}

func TestDecodeChatWithReasoningCarrierStreamsOnePreludeBeforeAssistantOutput(t *testing.T) {
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"trace\":\"think \"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"trace\":\"now\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	decoded, err := DecodeChatWithReasoningCarrier(
		context.Background(),
		Codec{Protocol: protocolkind.ChatCompletions},
		provider.Request{Attempt: provider.AttemptContext{ExchangeID: "exchange-stream"}, Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m")})},
		provider.StreamIngress{Stream: carrier.ByteStream{
			Header: http.Header{"Content-Type": {"text/event-stream"}}, MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw)),
		}},
		testChatReasoningExtractor{},
	)
	if err != nil {
		t.Fatal(err)
	}
	var events []canonical.Event
	for {
		event, nextErr := decoded.Stream.Next(context.Background())
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		events = append(events, event)
	}
	if err := decoded.Stream.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	preludeAt := -1
	firstAssistantAt := -1
	for index, event := range events {
		if event.ExchangeID != "exchange-stream" || event.Seq != int64(index+1) || event.Time.IsZero() {
			t.Fatalf("event %d identity = %#v", index, event)
		}
		item, isItem := event.Payload.(canonical.ItemEvent)
		if !isItem {
			continue
		}
		if event.Kind == canonical.EventItemCompleted {
			if completed, ok := item.Payload.(canonical.ItemCompletedPayload); ok {
				if reasoning, ok := completed.Item.Reasoning(); ok && reasoning.Parts()[0].Text() == "think now" {
					preludeAt = index
				}
			}
		}
		if event.Kind == canonical.EventItemStart {
			firstAssistantAt = index
			if item.Position.Item != 1 {
				t.Fatalf("assistant item position = %d, want 1", item.Position.Item)
			}
		}
	}
	if preludeAt < 0 || firstAssistantAt < 0 || preludeAt >= firstAssistantAt {
		t.Fatalf("prelude/assistant order = %d/%d; events=%#v", preludeAt, firstAssistantAt, events)
	}
}

func TestDecodeChatWithReasoningCarrierBuffersOnePreludeBeforeAssistantOutput(t *testing.T) {
	decoded, err := DecodeChatWithReasoningCarrier(
		context.Background(),
		Codec{Protocol: protocolkind.ChatCompletions},
		provider.Request{Attempt: provider.AttemptContext{ExchangeID: "exchange-buffered"}, Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m")})},
		provider.DocumentIngress{Document: carrier.NewDocument(
			protocolkind.ChatCompletions,
			"application/json",
			nil,
			[]byte(`{"id":"chat_1","model":"m","choices":[{"message":{"role":"assistant","trace":"think first","content":"answer"},"finish_reason":"stop"}]}`),
			carrier.Meta{},
		)},
		testChatReasoningExtractor{},
	)
	if err != nil {
		t.Fatal(err)
	}
	var events []canonical.Event
	for {
		event, nextErr := decoded.Stream.Next(context.Background())
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		events = append(events, event)
	}

	preludeAt := -1
	firstAssistantAt := -1
	for index, event := range events {
		item, isItem := event.Payload.(canonical.ItemEvent)
		if !isItem {
			continue
		}
		if event.Kind == canonical.EventItemCompleted {
			if completed, ok := item.Payload.(canonical.ItemCompletedPayload); ok {
				if reasoning, ok := completed.Item.Reasoning(); ok && reasoning.Parts()[0].Text() == "think first" {
					preludeAt = index
				}
			}
		}
		if event.Kind == canonical.EventItemStart {
			firstAssistantAt = index
			if item.Position.Item != 1 {
				t.Fatalf("assistant item position = %d, want 1", item.Position.Item)
			}
		}
	}
	if preludeAt < 0 || firstAssistantAt < 0 || preludeAt >= firstAssistantAt {
		t.Fatalf("prelude/assistant order = %d/%d; events=%#v", preludeAt, firstAssistantAt, events)
	}
}

func TestChatReasoningCarrierFailsAtExtractorAndClosesRawBody(t *testing.T) {
	document := carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, []byte(`{"choices":[{"message":{"trace":42}}]}`), carrier.Meta{})
	if _, _, err := ExtractChatReasoningDocument(document, testChatReasoningExtractor{}); err == nil {
		t.Fatal("malformed declared carrier did not fail")
	}
	streamed := NewChatReasoningSSEBody(io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"trace\":42}}]}\n\n")), testChatReasoningExtractor{})
	if _, err := io.ReadAll(streamed); err == nil {
		t.Fatal("malformed streamed carrier did not fail")
	}
	if err := streamed.Close(); err != nil {
		t.Fatal(err)
	}

	body := &testReadCloser{Reader: strings.NewReader("")}
	decoded, err := DecodeChatWithReasoningCarrier(
		context.Background(),
		Codec{Protocol: protocolkind.ChatCompletions},
		provider.Request{Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m")})},
		provider.StreamIngress{Stream: carrier.ByteStream{
			Header: http.Header{"Content-Type": {"text/event-stream"}}, MediaType: "text/event-stream", Body: body,
		}},
		testChatReasoningExtractor{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := decoded.Stream.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !body.closed {
		t.Fatal("closing shared stream did not close raw SSE body")
	}
}

type testChatReasoningExtractor struct{}

func (testChatReasoningExtractor) ExtractBufferedChatReasoning(message map[string]json.RawMessage) (string, error) {
	value, present := message["trace"]
	if !present {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", canonical.InternalError("test reasoning carrier is invalid")
	}
	delete(message, "trace")
	return text, nil
}

func (testChatReasoningExtractor) ExtractStreamedChatReasoning(delta map[string]json.RawMessage) (ChatReasoningFragment, error) {
	value, present := delta["trace"]
	if !present {
		return ChatReasoningFragment{}, nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return ChatReasoningFragment{}, canonical.InternalError("test streamed reasoning carrier is invalid")
	}
	delete(delta, "trace")
	return ChatReasoningFragment{Text: text, Observed: true}, nil
}

func (testChatReasoningExtractor) NewChatReasoningItem(text string) (canonical.CanonicalItem, error) {
	if text == "" {
		return canonical.CanonicalItem{}, nil
	}
	return testChatReasoningItem(nil, text), nil
}

func testChatReasoningItem(t *testing.T, text string) canonical.CanonicalItem {
	if t != nil {
		t.Helper()
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, text)
	if err != nil {
		if t != nil {
			t.Fatal(err)
		}
		panic(err)
	}
	item, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, canonical.OpaqueThinking{})
	if err != nil {
		if t != nil {
			t.Fatal(err)
		}
		panic(err)
	}
	return item
}

type testResponseStream struct {
	events []canonical.Event
	index  int
	closed bool
}

func (s *testResponseStream) Next(context.Context) (canonical.Event, error) {
	if s.index >= len(s.events) {
		return canonical.Event{}, io.EOF
	}
	event := s.events[s.index]
	s.index++
	return event, nil
}

func (s *testResponseStream) Close(context.Context) error {
	s.closed = true
	return nil
}

type testReadCloser struct {
	io.Reader
	closed bool
}

func (r *testReadCloser) Close() error {
	r.closed = true
	return nil
}

var _ ChatReasoningExtractor = testChatReasoningExtractor{}
