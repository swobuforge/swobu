package protocolcodec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// ChatVisibleReasoningExtractor separates client-visible reasoning from any
// provider-owned continuation state accumulated by the base extractor.
type ChatVisibleReasoningExtractor interface {
	NewChatVisibleReasoningItem(text string) (canonical.CanonicalItem, error)
}

// ChatLateVisibleReasoningExtractor explicitly admits a provider contract in
// which visible reasoning may form a terminal item after answer or tool output.
// Extractors without this capability retain the strict prelude-only invariant.
type ChatLateVisibleReasoningExtractor interface {
	NewChatLateVisibleReasoningItem(text string) (canonical.CanonicalItem, error)
}

// ChatContinuationReasoningExtractor owns opaque provider continuation state,
// whose lifecycle is independent of visible reasoning order.
type ChatContinuationReasoningExtractor interface {
	FinalizeChatContinuation() (canonical.CanonicalItem, error)
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
	reader     *core.SSEReaderCloser
	extractor  ChatReasoningExtractor
	buffer     bytes.Buffer
	held       bytes.Buffer
	eof        bool
	pendingErr error
	tailFrames int
	tailBytes  int

	mu                 sync.Mutex
	reasoning          bytes.Buffer
	lateReasoning      bytes.Buffer
	ready              canonical.CanonicalItem
	tail               canonical.CanonicalItem
	continuation       canonical.CanonicalItem
	preludeDone        bool
	visiblePayloadSeen bool
	lateVisibleOpen    bool
	continuationDone   bool
}

const (
	maxChatReasoningTailFrames = 64
	maxChatReasoningTailBytes  = 256 << 10
	maxChatLateReasoningBytes  = 256 << 10
)

// NewChatReasoningSSEBody constructs the Chat-only response transformation
// used before shared Chat stream decoding. The returned body owns closing the
// supplied raw body.
func NewChatReasoningSSEBody(body io.ReadCloser, extractor ChatReasoningExtractor) *ChatReasoningSSEBody {
	return &ChatReasoningSSEBody{reader: core.NewSSEReader(body), extractor: extractor}
}

func (b *ChatReasoningSSEBody) Read(output []byte) (int, error) {
	for b.buffer.Len() == 0 {
		if b.eof {
			if b.pendingErr != nil {
				err := b.pendingErr
				b.pendingErr = nil
				return 0, err
			}
			return 0, io.EOF
		}
		event, err := b.reader.Next(context.Background())
		if err != nil {
			if finishErr := b.completeFinal(); finishErr != nil {
				return 0, finishErr
			}
			_, _ = b.buffer.ReadFrom(&b.held)
			b.eof = true
			if !errors.Is(err, io.EOF) {
				b.pendingErr = err
			}
			if b.buffer.Len() == 0 {
				return 0, err
			}
			break
		}
		data := event.Data
		terminal := false
		if data != "[DONE]" {
			var chunk map[string]json.RawMessage
			if decodeErr := json.Unmarshal([]byte(data), &chunk); decodeErr == nil {
				var transformErr error
				terminal, transformErr = b.transform(chunk)
				if transformErr != nil {
					return 0, transformErr
				}
				encoded, _ := json.Marshal(chunk)
				data = string(encoded)
			}
		} else if err := b.completeFinal(); err != nil {
			return 0, err
		}
		var frame bytes.Buffer
		if event.Event != "" {
			fmt.Fprintf(&frame, "event: %s\n", event.Event)
		}
		fmt.Fprintf(&frame, "data: %s\n\n", data)
		if terminal || (b.held.Len() > 0 && data != "[DONE]") {
			b.tailFrames++
			b.tailBytes += frame.Len()
			if b.tailFrames > maxChatReasoningTailFrames || b.tailBytes > maxChatReasoningTailBytes {
				return 0, canonical.NewBackendError("", 0, "Chat reasoning terminal tail exceeded its bound", "")
			}
			_, _ = b.held.ReadFrom(&frame)
			continue
		}
		if data == "[DONE]" {
			_, _ = b.buffer.ReadFrom(&b.held)
		}
		_, _ = b.buffer.ReadFrom(&frame)
	}
	return b.buffer.Read(output)
}

func (b *ChatReasoningSSEBody) transform(chunk map[string]json.RawMessage) (bool, error) {
	var choices []json.RawMessage
	_ = json.Unmarshal(chunk["choices"], &choices)
	terminal := false
	for index, raw := range choices {
		var choice, delta map[string]json.RawMessage
		_ = json.Unmarshal(raw, &choice)
		_ = json.Unmarshal(choice["delta"], &delta)
		fragment, err := b.extractor.ExtractStreamedChatReasoning(delta)
		if err != nil {
			return false, err
		}
		preludeBoundaryReached := chatPreludeBoundaryReached(choice, delta)
		visiblePayloadStarted := chatVisiblePayloadStarted(delta)
		if fragment.Observed {
			b.mu.Lock()
			if b.preludeDone {
				_, admitted := b.extractor.(ChatLateVisibleReasoningExtractor)
				if !admitted {
					b.mu.Unlock()
					return false, canonical.InternalError("Chat streamed reasoning arrived after answer output")
				}
				if !b.visiblePayloadSeen {
					b.mu.Unlock()
					return false, canonical.InternalError("Chat streamed reasoning arrived after finish without answer or tool output")
				}
				if visiblePayloadStarted {
					b.mu.Unlock()
					return false, canonical.InternalError("Chat streamed reasoning and answer output arrived in one late frame")
				}
				if b.lateReasoning.Len()+len(fragment.Text) > maxChatLateReasoningBytes {
					b.mu.Unlock()
					return false, canonical.NewBackendError("", 0, "Chat late visible reasoning exceeded its bound", "")
				}
				b.lateReasoning.WriteString(fragment.Text)
				b.lateVisibleOpen = true
				b.mu.Unlock()
			} else {
				b.reasoning.WriteString(fragment.Text)
				b.mu.Unlock()
			}
		}
		b.mu.Lock()
		if visiblePayloadStarted {
			b.visiblePayloadSeen = true
		}
		lateVisibleOpen := b.lateVisibleOpen
		b.mu.Unlock()
		if visiblePayloadStarted && lateVisibleOpen {
			return false, canonical.InternalError("Chat answer output resumed after late reasoning")
		}
		if preludeBoundaryReached {
			if err := b.completePrelude(); err != nil {
				return false, err
			}
		}
		if len(choice["finish_reason"]) > 0 && string(choice["finish_reason"]) != "null" {
			terminal = true
		}
		choice["delta"], _ = json.Marshal(delta)
		choices[index], _ = json.Marshal(choice)
	}
	chunk["choices"], _ = json.Marshal(choices)
	return terminal, nil
}

func chatPreludeBoundaryReached(choice, delta map[string]json.RawMessage) bool {
	return chatVisiblePayloadStarted(delta) || (len(choice["finish_reason"]) > 0 && string(choice["finish_reason"]) != "null")
}

func chatVisiblePayloadStarted(delta map[string]json.RawMessage) bool {
	var content string
	_ = json.Unmarshal(delta["content"], &content)
	var calls []json.RawMessage
	_ = json.Unmarshal(delta["tool_calls"], &calls)
	return content != "" || len(calls) > 0
}

func (b *ChatReasoningSSEBody) completePrelude() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.preludeDone {
		return nil
	}
	visible, split := b.extractor.(ChatVisibleReasoningExtractor)
	var item canonical.CanonicalItem
	var err error
	if split {
		item, err = visible.NewChatVisibleReasoningItem(b.reasoning.String())
	} else {
		item, err = b.extractor.NewChatReasoningItem(b.reasoning.String())
	}
	if err != nil {
		return err
	}
	b.ready = item
	b.preludeDone = true
	return nil
}

func (b *ChatReasoningSSEBody) completeFinal() error {
	if err := b.completePrelude(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.continuationDone {
		return nil
	}
	if b.lateReasoning.Len() > 0 {
		late, ok := b.extractor.(ChatLateVisibleReasoningExtractor)
		if !ok {
			return canonical.InternalError("Chat late visible reasoning was not admitted")
		}
		item, err := late.NewChatLateVisibleReasoningItem(b.lateReasoning.String())
		if err != nil {
			return err
		}
		b.tail = item
	}
	if continuation, ok := b.extractor.(ChatContinuationReasoningExtractor); ok {
		item, err := continuation.FinalizeChatContinuation()
		if err != nil {
			return err
		}
		b.continuation = item
	}
	b.continuationDone = true
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

// TakeTail returns visible reasoning that the provider delivered after answer
// or tool output. It is a separate ordered response item rather than part of
// the prelude, and becomes available only after the provider stream settles.
func (b *ChatReasoningSSEBody) TakeTail() (canonical.CanonicalItem, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tail.Kind() == "" {
		return canonical.CanonicalItem{}, false
	}
	item := b.tail
	b.tail = canonical.CanonicalItem{}
	return item, true
}

func (b *ChatReasoningSSEBody) checkpointResponse(response canonical.CanonicalResponse) (canonical.CanonicalResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.continuationDone {
		return canonical.CanonicalResponse{}, canonical.InternalError("Chat continuation was not finalized")
	}
	if b.continuation.Kind() == "" {
		return response, nil
	}
	return response.WithReasoningPrelude(b.continuation)
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
		decoded, err := standard.decodeBase(ctx, request, provider.DocumentIngress{Document: cleaned})
		if err != nil || item.Kind() == "" {
			return decoded, err
		}
		decoded.Stream = newChatReasoningPreludeStream(decoded.Stream, item, nil, request.Attempt.ExchangeID)
		return decoded, nil
	case provider.StreamIngress:
		body := NewChatReasoningSSEBody(value.Stream.Body, extractor)
		cleaned := value.Stream
		cleaned.Body = body
		decoded, err := standard.decodeBase(ctx, request, provider.StreamIngress{Stream: cleaned})
		if err != nil {
			_ = body.Close()
			return decoded, err
		}
		decoded.Stream = newChatReasoningPreludeStream(decoded.Stream, canonical.CanonicalItem{}, body, request.Attempt.ExchangeID)
		if _, ok := extractor.(ChatContinuationReasoningExtractor); ok {
			decoded.CheckpointResponse = body.checkpointResponse
		}
		return decoded, nil
	default:
		return provider.DecodedResponse{}, fmt.Errorf("Chat reasoning carrier ingress %T is unsupported", ingress)
	}
}

type readyChatReasoningSource interface {
	Take() (canonical.CanonicalItem, bool)
	TakeTail() (canonical.CanonicalItem, bool)
}

type chatReasoningPreludeStream struct {
	upstream    canonical.ResponseStream
	source      readyChatReasoningSource
	ready       canonical.CanonicalItem
	pending     []canonical.Event
	emitted     bool
	tailEmitted bool
	nextItem    uint32
	seq         int64
	exchangeID  string
}

func newChatReasoningPreludeStream(upstream canonical.ResponseStream, item canonical.CanonicalItem, source readyChatReasoningSource, exchangeID string) *chatReasoningPreludeStream {
	return &chatReasoningPreludeStream{upstream: upstream, source: source, ready: item, exchangeID: exchangeID}
}

func (s *chatReasoningPreludeStream) Next(ctx context.Context) (canonical.Event, error) {
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		s.observeItem(event)
		return s.finish(event), nil
	}
	for {
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
				s.pending = append(s.pending, shiftChatReasoningItem(event))
				return s.finish(chatReasoningItemCompleted(item)), nil
			}
		}
		if !s.tailEmitted && closesChatReasoningOutput(event) && s.source != nil {
			if item, ok := s.source.TakeTail(); ok {
				s.tailEmitted = true
				s.pending = append(s.pending, event)
				return s.finish(chatReasoningItemCompletedAt(item, s.nextItem)), nil
			}
		}
		if s.emitted {
			event = shiftChatReasoningItem(event)
		}
		s.observeItem(event)
		return s.finish(event), nil
	}
}

func (s *chatReasoningPreludeStream) observeItem(event canonical.Event) {
	item, ok := event.Payload.(canonical.ItemEvent)
	if !ok {
		return
	}
	next := item.Position.Item + 1
	if next > s.nextItem {
		s.nextItem = next
	}
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
	return chatReasoningItemCompletedAt(item, 0)
}

func chatReasoningItemCompletedAt(item canonical.CanonicalItem, ordinal uint32) canonical.Event {
	return canonical.Event{
		Kind: canonical.EventItemCompleted,
		Payload: canonical.ItemEvent{
			Position: canonical.ItemPosition{Item: ordinal},
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

func closesChatReasoningOutput(event canonical.Event) bool {
	switch event.Kind {
	case canonical.EventUsage, canonical.EventFinish, canonical.EventEnvelopeEnd, canonical.EventError:
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
