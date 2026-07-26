package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
	"github.com/swobuforge/swobu/internal/wire/responses"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

// reasoningCodec owns the OpenRouter dialect around an otherwise standard
// Chat Completions codec. The wrapped protocol codec never observes these
// provider fields.
type reasoningCodec struct{ standard protocolcodec.Codec }

func (c reasoningCodec) Encode(req provider.Request) (carrier.Document, []compat.Decision, error) {
	if err := protocolcodec.ValidateEncodeRequest(req); err != nil {
		return carrier.Document{}, nil, err
	}
	document, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (chatcompletions.ProviderRequestDocument, error) {
		return chatcompletions.LowerProviderRequestDocument(req.Canonical, req.Delivery, sink, "")
	})
	if err != nil {
		return carrier.Document{}, decisions, err
	}
	delete(document.Payload, "reasoning_effort")
	if err := applyOpenRouterReasoningRequest(document.Payload, req.Canonical); err != nil {
		return carrier.Document{}, decisions, err
	}
	if err := decorateOpenRouterThinking(&document, req.Canonical.Items()); err != nil {
		return carrier.Document{}, decisions, err
	}
	environment, err := canonical.EffectiveTools(req.Canonical)
	if err != nil {
		return carrier.Document{}, decisions, err
	}
	delete(document.Payload, "web_search_options")
	if hasCanonicalWebSearch(environment.Declarations()) {
		document.Tools = append(document.Tools, chatcompletions.ProviderRequestTool{Type: "openrouter:web_search"})
	}
	replaceOpenRouterWebSearchChoice(document.ToolChoice)
	encoded, err := chatcompletions.EncodeProviderRequestDocument(document)
	return encoded, decisions, err
}

func hasCanonicalWebSearch(tools []canonical.ToolDeclaration) bool {
	for _, tool := range tools {
		if tool.Kind() == canonical.ToolKindWebSearch {
			return true
		}
	}
	return false
}

func (c reasoningCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	switch value := ingress.(type) {
	case provider.DocumentIngress:
		cleaned, reasoning, err := extractBufferedOpenRouterReasoning(value.Document)
		if err != nil {
			return provider.DecodedResponse{}, err
		}
		decoded, err := c.standard.Decode(ctx, req, provider.DocumentIngress{Document: cleaned})
		if err != nil || reasoning.Kind() == "" {
			return decoded, err
		}
		decoded.Stream = newAtomicReasoningStream(decoded.Stream, reasoning, req.ExchangeID)
		return decoded, nil
	case provider.StreamIngress:
		body := newOpenRouterSSEBody(value.Stream.Body)
		cleaned := value.Stream
		cleaned.Body = body
		decoded, err := c.standard.Decode(ctx, req, provider.StreamIngress{Stream: cleaned})
		if err != nil {
			_ = body.Close()
			return decoded, err
		}
		decoded.Stream = &atomicReasoningResponseStream{upstream: decoded.Stream, source: body, exchangeID: req.ExchangeID}
		return decoded, nil
	default:
		return provider.DecodedResponse{}, fmt.Errorf("OpenRouter ingress %T is unsupported", ingress)
	}
}

func applyOpenRouterReasoningRequest(payload map[string]any, req canonical.CanonicalRequest) error {
	out := map[string]any{}
	effort, effortSet := req.Controls().Effort.Get()
	if compute, set := req.Reasoning().ComputeField().Get(); set {
		switch compute.Kind() {
		case canonical.ReasoningDisabled:
			if effortSet {
				return provider.NewIncompatibleTarget("OpenRouter target cannot combine disabled canonical reasoning with inference effort")
			}
			out["enabled"] = false
		case canonical.ReasoningAutomatic:
			out["enabled"] = true
		case canonical.ReasoningBudget:
			tokens, _ := compute.Tokens()
			out["max_tokens"] = tokens
		default:
			return canonical.BadRequest("reasoning compute is invalid")
		}
	}
	if effortSet {
		out["effort"] = string(effort)
	}
	if disclosure, set := req.Reasoning().DisclosureField().Get(); set {
		// Keep backend capture independent whenever this request may open a tool
		// continuation; client projection enforces disclosure again.
		canContinue, err := canOpenToolContinuation(req)
		if err != nil {
			return err
		}
		if disclosure == canonical.ReasoningDisclosureNone && !canContinue {
			out["exclude"] = true
		}
	}
	if len(out) > 0 {
		payload["reasoning"] = out
	}
	return nil
}

func canOpenToolContinuation(req canonical.CanonicalRequest) (bool, error) {
	environment, err := canonical.EffectiveTools(req)
	if err != nil {
		return false, err
	}
	if len(environment.Declarations()) == 0 {
		return false, nil
	}
	policy, err := req.EffectiveToolPolicy()
	if err != nil {
		return false, err
	}
	return policy.Mode != canonical.ToolPolicyNone, nil
}

type openRouterRequestMessage struct {
	chatcompletions.ProviderRequestMessage
	ReasoningDetails json.RawMessage `json:"reasoning_details,omitempty"`
}

func decorateOpenRouterThinking(document *chatcompletions.ProviderRequestDocument, items []canonical.CanonicalItem) error {
	messages := make([]openRouterRequestMessage, len(document.Messages))
	for index, message := range document.Messages {
		messages[index].ProviderRequestMessage = message
		if message.Role != "assistant" || message.SourceStart < 0 {
			continue
		}
		for source := message.SourceStart; source < message.SourceEnd && source < len(items); source++ {
			reasoning, ok := items[source].Reasoning()
			if !ok {
				continue
			}
			if opaque, ok := reasoning.Opaque().OpenRouter(); ok {
				if !json.Valid(opaque) {
					return canonical.InternalError("checkpoint contains invalid OpenRouter opaque thinking")
				}
				messages[index].ReasoningDetails = json.RawMessage(opaque)
			}
		}
	}
	document.Payload["messages"] = messages
	return nil
}

func extractBufferedOpenRouterReasoning(document carrier.Document) (carrier.Document, canonical.CanonicalItem, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(document.RawBytes(), &root); err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("OpenRouter response is invalid JSON")
	}
	var choices []json.RawMessage
	if err := json.Unmarshal(root["choices"], &choices); err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("OpenRouter response is invalid JSON")
	}
	if len(choices) == 0 {
		return document, canonical.CanonicalItem{}, nil
	}
	var choice map[string]json.RawMessage
	if err := json.Unmarshal(choices[0], &choice); err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("OpenRouter response choice is invalid")
	}
	var message map[string]json.RawMessage
	if err := json.Unmarshal(choice["message"], &message); err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("OpenRouter response message is invalid")
	}
	details, hasDetails := message["reasoning_details"]
	var flat string
	if rawFlat, ok := message["reasoning"]; ok {
		_ = json.Unmarshal(rawFlat, &flat)
	}
	delete(message, "reasoning_details")
	delete(message, "reasoning")
	item, err := newOpenRouterReasoningItem(details, hasDetails, flat)
	if err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, err
	}
	choice["message"], err = json.Marshal(message)
	if err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("OpenRouter response message could not be normalized")
	}
	choices[0], err = json.Marshal(choice)
	if err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("OpenRouter response choice could not be normalized")
	}
	root["choices"], err = json.Marshal(choices)
	if err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("OpenRouter response choices could not be normalized")
	}
	raw, err := json.Marshal(root)
	if err != nil {
		return carrier.Document{}, canonical.CanonicalItem{}, canonical.InternalError("OpenRouter response could not be normalized")
	}
	return carrier.NewDocument(document.Family, document.Media, document.Header, raw, document.Meta), item, nil
}

func newOpenRouterReasoningItem(details json.RawMessage, hasDetails bool, flat string) (canonical.CanonicalItem, error) {
	parts := make([]canonical.ReasoningPart, 0)
	var opaque canonical.OpaqueThinking
	if hasDetails {
		if !json.Valid(details) {
			return canonical.CanonicalItem{}, canonical.InternalError("OpenRouter reasoning_details are invalid")
		}
		value, err := canonical.NewOpenRouterOpaqueThinking(details)
		if err != nil {
			return canonical.CanonicalItem{}, err
		}
		opaque = value
		parts = append(parts, portableOpenRouterParts(details)...)
	}
	if flat != "" && len(parts) == 0 {
		part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, flat)
		if err != nil {
			return canonical.CanonicalItem{}, err
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 && opaque.IsZero() {
		return canonical.CanonicalItem{}, nil
	}
	return canonical.NewReasoningItem(parts, opaque)
}

func portableOpenRouterParts(details json.RawMessage) []canonical.ReasoningPart {
	var entries []struct {
		Text    string `json:"text"`
		Summary string `json:"summary"`
	}
	if json.Unmarshal(details, &entries) != nil {
		return nil
	}
	parts := make([]canonical.ReasoningPart, 0, len(entries))
	for _, entry := range entries {
		kind := canonical.ReasoningPartTrace
		text := entry.Text
		if entry.Summary != "" {
			kind, text = canonical.ReasoningPartSummary, entry.Summary
		}
		if text == "" {
			continue
		}
		part, err := canonical.NewReasoningPart(kind, text)
		if err == nil {
			parts = append(parts, part)
		}
	}
	return parts
}

type atomicReasoningResponseStream struct {
	upstream   canonical.ResponseStream
	source     *openRouterSSEReadCloser
	ready      canonical.CanonicalItem
	pending    *canonical.Event
	emitted    bool
	seq        int64
	exchangeID string
}

func newAtomicReasoningStream(upstream canonical.ResponseStream, item canonical.CanonicalItem, exchangeID string) *atomicReasoningResponseStream {
	return &atomicReasoningResponseStream{upstream: upstream, ready: item, exchangeID: exchangeID}
}

func (s *atomicReasoningResponseStream) Next(ctx context.Context) (canonical.Event, error) {
	if s.pending != nil {
		event := *s.pending
		s.pending = nil
		if s.emitted {
			event = shiftItemOrdinal(event)
		}
		return s.finish(event), nil
	}
	event, err := s.upstream.Next(ctx)
	if err != nil {
		if err == io.EOF && !s.emitted && s.source != nil {
			if item, ok := s.source.takeReasoning(); ok {
				s.emitted = true
				return s.finish(reasoningCompletedEvent(item)), nil
			}
		}
		return canonical.Event{}, err
	}
	if !s.emitted && opensResponseItemOrTerminal(event) {
		item, ok := s.availableReasoning()
		if ok {
			s.emitted = true
			s.pending = &event
			return s.finish(reasoningCompletedEvent(item)), nil
		}
	}
	if s.emitted {
		event = shiftItemOrdinal(event)
	}
	return s.finish(event), nil
}

func (s *atomicReasoningResponseStream) finish(event canonical.Event) canonical.Event {
	s.seq++
	event.ExchangeID = s.exchangeID
	event.Seq = s.seq
	event.Time = time.Now().UTC()
	return event
}

func (s *atomicReasoningResponseStream) availableReasoning() (canonical.CanonicalItem, bool) {
	if s.ready.Kind() != "" {
		item := s.ready
		s.ready = canonical.CanonicalItem{}
		return item, true
	}
	if s.source != nil {
		return s.source.takeReasoning()
	}
	return canonical.CanonicalItem{}, false
}

func (s *atomicReasoningResponseStream) Close(ctx context.Context) error {
	return s.upstream.Close(ctx)
}

func reasoningCompletedEvent(item canonical.CanonicalItem) canonical.Event {
	return canonical.Event{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{
		Position: canonical.ItemPosition{Item: 0}, Payload: canonical.ItemCompletedPayload{Item: item},
	}}
}

func opensResponseItemOrTerminal(event canonical.Event) bool {
	switch event.Kind {
	case canonical.EventItemStart, canonical.EventItemCompleted, canonical.EventUsage,
		canonical.EventFinish, canonical.EventEnvelopeEnd, canonical.EventError:
		return true
	default:
		return false
	}
}

func shiftItemOrdinal(event canonical.Event) canonical.Event {
	item, ok := event.Payload.(canonical.ItemEvent)
	if !ok {
		return event
	}
	item.Position.Item++
	event.Payload = item
	return event
}

type openRouterSSEReadCloser struct {
	reader  *core.SSEReaderCloser
	buffer  bytes.Buffer
	mu      sync.Mutex
	details []json.RawMessage
	flat    bytes.Buffer
	ready   canonical.CanonicalItem
	done    bool
}

func newOpenRouterSSEBody(body io.ReadCloser) *openRouterSSEReadCloser {
	return &openRouterSSEReadCloser{reader: core.NewSSEReader(body)}
}

func (b *openRouterSSEReadCloser) Read(output []byte) (int, error) {
	for b.buffer.Len() == 0 {
		event, err := b.reader.Next(context.Background())
		if err != nil {
			if completeErr := b.complete(); completeErr != nil {
				return 0, completeErr
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
		} else {
			if err := b.complete(); err != nil {
				return 0, err
			}
		}
		if event.Event != "" {
			fmt.Fprintf(&b.buffer, "event: %s\n", event.Event)
		}
		fmt.Fprintf(&b.buffer, "data: %s\n\n", data)
	}
	return b.buffer.Read(output)
}

func (b *openRouterSSEReadCloser) transform(chunk map[string]json.RawMessage) error {
	var choices []json.RawMessage
	_ = json.Unmarshal(chunk["choices"], &choices)
	for index, value := range choices {
		var choice map[string]json.RawMessage
		_ = json.Unmarshal(value, &choice)
		var delta map[string]json.RawMessage
		_ = json.Unmarshal(choice["delta"], &delta)
		var details []json.RawMessage
		if rawDetails, ok := delta["reasoning_details"]; ok && json.Unmarshal(rawDetails, &details) == nil {
			b.mu.Lock()
			if b.done {
				b.mu.Unlock()
				return canonical.InternalError("OpenRouter streamed reasoning arrived after answer output")
			}
			b.details = append(b.details, details...)
			b.mu.Unlock()
		}
		var text string
		if rawText, ok := delta["reasoning"]; ok && json.Unmarshal(rawText, &text) == nil {
			b.mu.Lock()
			if b.done {
				b.mu.Unlock()
				return canonical.InternalError("OpenRouter streamed reasoning arrived after answer output")
			}
			b.flat.WriteString(text)
			b.mu.Unlock()
		}
		delete(delta, "reasoning_details")
		delete(delta, "reasoning")
		var content string
		_ = json.Unmarshal(delta["content"], &content)
		var toolCalls []json.RawMessage
		_ = json.Unmarshal(delta["tool_calls"], &toolCalls)
		finishReason := choice["finish_reason"]
		if content != "" || len(toolCalls) > 0 || len(finishReason) > 0 && string(finishReason) != "null" {
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

func (b *openRouterSSEReadCloser) complete() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		return nil
	}
	hasDetails := len(b.details) > 0
	var details json.RawMessage
	if hasDetails {
		details, _ = json.Marshal(b.details)
	}
	item, err := newOpenRouterReasoningItem(details, hasDetails, b.flat.String())
	if err != nil {
		return err
	}
	b.ready = item
	b.done = true
	return nil
}

func (b *openRouterSSEReadCloser) takeReasoning() (canonical.CanonicalItem, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ready.Kind() == "" {
		return canonical.CanonicalItem{}, false
	}
	item := b.ready
	b.ready = canonical.CanonicalItem{}
	return item, true
}

func (b *openRouterSSEReadCloser) Close() error { return b.reader.Close() }

var _ provider.Codec = reasoningCodec{}

// responsesCodec owns OpenRouter's exact Responses request composition while
// the standard codec retains shared response decoding.
type responsesCodec struct{ standard protocolcodec.Codec }

func (c responsesCodec) Encode(req provider.Request) (carrier.Document, []compat.Decision, error) {
	if err := protocolcodec.ValidateEncodeRequest(req); err != nil {
		return carrier.Document{}, nil, err
	}
	document, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (responses.ProviderRequestDocument, error) {
		return responses.LowerProviderRequestDocument(
			responses.EncodeInput{Request: req.Canonical},
			req.Delivery,
			sink,
			req.ExchangeID,
			responses.EncodeOptions{},
		)
	})
	if err != nil {
		return carrier.Document{}, decisions, err
	}
	for index := range document.Tools {
		if document.Tools[index].Type == canonical.ToolTypeWebSearch {
			document.Tools[index].Type = "openrouter:web_search"
		}
	}
	replaceOpenRouterWebSearchChoice(document.ToolChoice)
	encoded, err := responses.EncodeProviderRequestDocument(document)
	return encoded, decisions, err
}

func (c responsesCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return c.standard.Decode(ctx, req, ingress)
}

var _ provider.Codec = responsesCodec{}

func replaceOpenRouterWebSearchChoice(choice any) {
	object, ok := choice.(map[string]any)
	if ok && object["type"] == canonical.ToolTypeWebSearch {
		object["type"] = "openrouter:web_search"
	}
}
