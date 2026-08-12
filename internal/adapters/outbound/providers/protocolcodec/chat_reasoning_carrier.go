package protocolcodec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

// ChatReasoningFragment is one provider-extracted piece of a streamed Chat
// reasoning carrier. Observed distinguishes an absent carrier from a carrier
// whose valid text happens to be empty.
type ChatReasoningFragment struct {
	Text     string
	Observed bool
}

// ChatReasoningExtractor keeps provider-owned Chat carrier spelling,
// validation, and canonical-item construction at the adapter edge. It must
// remove its carrier from message and delta maps before returning so the shared
// Chat decoder receives only ordinary Chat fields.
type ChatReasoningExtractor interface {
	ExtractBufferedChatReasoning(message map[string]json.RawMessage) (string, error)
	ExtractStreamedChatReasoning(delta map[string]json.RawMessage) (ChatReasoningFragment, error)
	NewChatReasoningItem(text string) (canonical.CanonicalItem, error)
}

// ExtractChatReasoningDocument removes one provider-owned reasoning carrier
// from the first Chat choice and turns it into one canonical item. Structural
// Chat envelope traversal belongs here; carrier field interpretation remains
// in extractor.
func ExtractChatReasoningDocument(document carrier.Document, extractor ChatReasoningExtractor) (carrier.Document, canonical.CanonicalItem, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(document.RawBytes(), &root); err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("Chat reasoning response is invalid JSON")
	}
	var choices []json.RawMessage
	if err := json.Unmarshal(root["choices"], &choices); err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("Chat reasoning response choices are invalid")
	}
	if len(choices) == 0 {
		return document, canonical.CanonicalItem{}, nil
	}
	var choice, message map[string]json.RawMessage
	if err := json.Unmarshal(choices[0], &choice); err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("Chat reasoning response choice is invalid")
	}
	if err := json.Unmarshal(choice["message"], &message); err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("Chat reasoning response message is invalid")
	}
	text, err := extractor.ExtractBufferedChatReasoning(message)
	if err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, err
	}
	item, err := extractor.NewChatReasoningItem(text)
	if err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, err
	}
	choice["message"], err = json.Marshal(message)
	if err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("Chat reasoning response message could not be normalized")
	}
	choices[0], err = json.Marshal(choice)
	if err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("Chat reasoning response choice could not be normalized")
	}
	root["choices"], err = json.Marshal(choices)
	if err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("Chat reasoning response choices could not be normalized")
	}
	raw, err := json.Marshal(root)
	if err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("Chat reasoning response could not be normalized")
	}
	return carrier.NewDocument(document.Family, document.Media, document.Header, raw, document.Meta), item, nil
}

// ChatReasoningSSEBody removes a provider-owned streamed Chat reasoning
// carrier while preserving the original SSE framing for the shared decoder.
// Take exposes the one accumulated canonical item after visible output or
// stream completion has made the reasoning prelude ready.
type ChatReasoningSSEBody struct {
	reader    *core.SSEReaderCloser
	extractor ChatReasoningExtractor
	buffer    bytes.Buffer

	mu        sync.Mutex
	reasoning bytes.Buffer
	ready     canonical.CanonicalItem
	done      bool
}

// NewChatReasoningSSEBody constructs the Chat-only response transformation
// used before shared Chat stream decoding. The returned body owns closing the
// supplied raw body.
func NewChatReasoningSSEBody(body io.ReadCloser, extractor ChatReasoningExtractor) *ChatReasoningSSEBody {
	return &ChatReasoningSSEBody{reader: core.NewSSEReader(body), extractor: extractor}
}

func (b *ChatReasoningSSEBody) Read(output []byte) (int, error) {
	for b.buffer.Len() == 0 {
		event, err := b.reader.Next(context.Background())
		if err != nil {
			if finishErr := b.complete(); finishErr != nil {
				return 0, finishErr
			}
			return 0, err
		}
		data := event.Data
		if data != "[DONE]" {
			var chunk map[string]json.RawMessage
			if json.Unmarshal([]byte(data), &chunk) == nil {
				if err := b.transform(chunk); err != nil {
					return 0, err
				}
				encoded, _ := json.Marshal(chunk)
				data = string(encoded)
			}
		} else if err := b.complete(); err != nil {
			return 0, err
		}
		if event.Event != "" {
			fmt.Fprintf(&b.buffer, "event: %s\n", event.Event)
		}
		fmt.Fprintf(&b.buffer, "data: %s\n\n", data)
	}
	return b.buffer.Read(output)
}

func (b *ChatReasoningSSEBody) transform(chunk map[string]json.RawMessage) error {
	var choices []json.RawMessage
	_ = json.Unmarshal(chunk["choices"], &choices)
	for index, raw := range choices {
		var choice, delta map[string]json.RawMessage
		_ = json.Unmarshal(raw, &choice)
		_ = json.Unmarshal(choice["delta"], &delta)
		fragment, err := b.extractor.ExtractStreamedChatReasoning(delta)
		if err != nil {
			return err
		}
		if fragment.Observed {
			b.mu.Lock()
			if b.done {
				b.mu.Unlock()
				return canonical.InternalError("Chat streamed reasoning arrived after answer output")
			}
			b.reasoning.WriteString(fragment.Text)
			b.mu.Unlock()
		}
		if chatOutputStarted(choice, delta) {
			if err := b.complete(); err != nil {
				return err
			}
		}
		choice["delta"], _ = json.Marshal(delta)
		choices[index], _ = json.Marshal(choice)
	}
	chunk["choices"], _ = json.Marshal(choices)
	return nil
}

func chatOutputStarted(choice, delta map[string]json.RawMessage) bool {
	var content string
	_ = json.Unmarshal(delta["content"], &content)
	var calls []json.RawMessage
	_ = json.Unmarshal(delta["tool_calls"], &calls)
	return content != "" || len(calls) > 0 || (len(choice["finish_reason"]) > 0 && string(choice["finish_reason"]) != "null")
}

func (b *ChatReasoningSSEBody) complete() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		return nil
	}
	item, err := b.extractor.NewChatReasoningItem(b.reasoning.String())
	if err != nil {
		return err
	}
	b.ready = item
	b.done = true
	return nil
}

// Take returns the completed reasoning item once. It is ready when a visible
// Chat output begins or when the SSE stream completes.
func (b *ChatReasoningSSEBody) Take() (canonical.CanonicalItem, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ready.Kind() == "" {
		return canonical.CanonicalItem{}, false
	}
	item := b.ready
	b.ready = canonical.CanonicalItem{}
	return item, true
}

// Close releases the raw SSE body held by this response transformation.
func (b *ChatReasoningSSEBody) Close() error { return b.reader.Close() }

// DecodeChatWithReasoningCarrier composes one provider-owned Chat reasoning
// carrier around a standard Chat codec. The prelude is emitted before the
// first assistant or tool event, while subsequent item positions are shifted
// and all emitted events are resequenced for the exchange.
func DecodeChatWithReasoningCarrier(ctx context.Context, standard Codec, request provider.Request, ingress provider.Ingress, extractor ChatReasoningExtractor) (provider.DecodedResponse, error) {
	switch value := ingress.(type) {
	case provider.DocumentIngress:
		cleaned, item, err := ExtractChatReasoningDocument(value.Document, extractor)
		if err != nil {
			return provider.DecodedResponse{}, err
		}
		decoded, err := standard.Decode(ctx, request, provider.DocumentIngress{Document: cleaned})
		if err != nil || item.Kind() == "" {
			return decoded, err
		}
		decoded.Stream = newChatReasoningPreludeStream(decoded.Stream, item, nil, request.ExchangeID)
		return decoded, nil
	case provider.StreamIngress:
		body := NewChatReasoningSSEBody(value.Stream.Body, extractor)
		cleaned := value.Stream
		cleaned.Body = body
		decoded, err := standard.Decode(ctx, request, provider.StreamIngress{Stream: cleaned})
		if err != nil {
			_ = body.Close()
			return decoded, err
		}
		decoded.Stream = newChatReasoningPreludeStream(decoded.Stream, canonical.CanonicalItem{}, body, request.ExchangeID)
		return decoded, nil
	default:
		return provider.DecodedResponse{}, fmt.Errorf("Chat reasoning carrier ingress %T is unsupported", ingress)
	}
}

type readyChatReasoningSource interface {
	Take() (canonical.CanonicalItem, bool)
}

type chatReasoningPreludeStream struct {
	upstream   canonical.ResponseStream
	source     readyChatReasoningSource
	ready      canonical.CanonicalItem
	pending    *canonical.Event
	emitted    bool
	seq        int64
	exchangeID string
}

func newChatReasoningPreludeStream(upstream canonical.ResponseStream, item canonical.CanonicalItem, source readyChatReasoningSource, exchangeID string) *chatReasoningPreludeStream {
	return &chatReasoningPreludeStream{upstream: upstream, source: source, ready: item, exchangeID: exchangeID}
}

func (s *chatReasoningPreludeStream) Next(ctx context.Context) (canonical.Event, error) {
	if s.pending != nil {
		event := shiftChatReasoningItem(*s.pending)
		s.pending = nil
		return s.finish(event), nil
	}
	event, err := s.upstream.Next(ctx)
	if err != nil {
		if err == io.EOF && !s.emitted && s.source != nil {
			if item, ok := s.source.Take(); ok {
				s.emitted = true
				return s.finish(chatReasoningItemCompleted(item)), nil
			}
		}
		return canonical.Event{}, err
	}
	if !s.emitted && opensChatReasoningPrelude(event) {
		if item, ok := s.available(); ok {
			// The reasoning checkpoint must precede its assistant/tool successor;
			// that successor is shifted only after this prelude is emitted.
			s.emitted = true
			s.pending = &event
			return s.finish(chatReasoningItemCompleted(item)), nil
		}
	}
	if s.emitted {
		event = shiftChatReasoningItem(event)
	}
	return s.finish(event), nil
}

func (s *chatReasoningPreludeStream) available() (canonical.CanonicalItem, bool) {
	if s.ready.Kind() != "" {
		item := s.ready
		s.ready = canonical.CanonicalItem{}
		return item, true
	}
	if s.source != nil {
		return s.source.Take()
	}
	return canonical.CanonicalItem{}, false
}

func (s *chatReasoningPreludeStream) finish(event canonical.Event) canonical.Event {
	s.seq++
	event.ExchangeID = s.exchangeID
	event.Seq = s.seq
	event.Time = time.Now().UTC()
	return event
}

// Close delegates to the shared decoder stream, which owns the transformed
// SSE body and therefore the underlying response-body close exactly once.
func (s *chatReasoningPreludeStream) Close(ctx context.Context) error { return s.upstream.Close(ctx) }

func chatReasoningItemCompleted(item canonical.CanonicalItem) canonical.Event {
	return canonical.Event{
		Kind: canonical.EventItemCompleted,
		Payload: canonical.ItemEvent{
			Position: canonical.ItemPosition{Item: 0},
			Payload:  canonical.ItemCompletedPayload{Item: item},
		},
	}
}

func opensChatReasoningPrelude(event canonical.Event) bool {
	switch event.Kind {
	case canonical.EventItemStart, canonical.EventItemCompleted, canonical.EventUsage, canonical.EventFinish, canonical.EventEnvelopeEnd, canonical.EventError:
		return true
	default:
		return false
	}
}

func shiftChatReasoningItem(event canonical.Event) canonical.Event {
	if item, ok := event.Payload.(canonical.ItemEvent); ok {
		item.Position.Item++
		event.Payload = item
	}
	return event
}

var _ io.ReadCloser = (*ChatReasoningSSEBody)(nil)
var _ canonical.ResponseStream = (*chatReasoningPreludeStream)(nil)
