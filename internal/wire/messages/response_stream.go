package messages

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	deliverycompat "github.com/swobuforge/swobu/internal/wire/deliverycompat"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

// DecodeResponseStream returns canonical envelope events directly for messages streams.
func decodeResponseStream(request canonical.CanonicalRequest, stream carrier.ByteStream, exchangeID string, sink compat.Sink) *messagesEventReader {
	recording := &compat.RecordingSink{Delegate: sink}
	return &messagesEventReader{
		exchangeID:  exchangeID,
		responseID:  canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		sink:        recording,
		recording:   recording,
		reader:      core.NewSSEReader(stream.Body),
		blocks:      map[int]*streamContentBlock{},
		latestUsage: canonical.NewUnknownTokenUsage(),
		request:     request.Clone(),
	}
}

type messagesEventReader struct {
	exchangeID   string
	responseID   canonical.EnvelopeID
	sink         compat.Sink
	recording    *compat.RecordingSink
	reader       *core.SSEReaderCloser
	resultID     string
	model        string
	finishReason string
	started      bool
	pending      canonical.EventSequence
	blocks       map[int]*streamContentBlock
	latestUsage  canonical.TokenUsage
	seq          int64
	completed    bool
	request      canonical.CanonicalRequest
}

func (s *messagesEventReader) Decisions() []compat.Decision {
	if s.recording == nil {
		return nil
	}
	return s.recording.Decisions()
}

type streamContentBlock struct {
	ItemKind      canonical.ItemKind
	CallID        canonical.ToolCallID
	Tool          canonical.ToolKey
	text          strings.Builder
	args          strings.Builder
	initialInput  json.RawMessage
	reasoningType string
	signature     strings.Builder
	data          string
	searchResult  json.RawMessage
	searchError   bool
	citations     []messagesCitationDTO
}

type streamEnvelope struct {
	Type string `json:"type"`
}

type messageStartFrame struct {
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	} `json:"message"`
}

type contentBlockStartFrame struct {
	Index        int `json:"index"`
	ContentBlock struct {
		Type      string                `json:"type"`
		ID        string                `json:"id"`
		Name      string                `json:"name"`
		Input     json.RawMessage       `json:"input"`
		Thinking  string                `json:"thinking"`
		Signature string                `json:"signature"`
		Data      string                `json:"data"`
		ToolUseID string                `json:"tool_use_id"`
		Content   json.RawMessage       `json:"content"`
		IsError   bool                  `json:"is_error"`
		Citations []messagesCitationDTO `json:"citations"`
	} `json:"content_block"`
}

type contentBlockDeltaFrame struct {
	Index int `json:"index"`
	Delta struct {
		Type        string              `json:"type"`
		Text        string              `json:"text"`
		PartialJSON string              `json:"partial_json"`
		Thinking    string              `json:"thinking"`
		Signature   string              `json:"signature"`
		Citation    messagesCitationDTO `json:"citation"`
	} `json:"delta"`
}

type contentBlockStopFrame struct {
	Index int `json:"index"`
}

type messageDeltaFrame struct {
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
}

func (s *messagesEventReader) Next(ctx context.Context) (canonical.Event, error) {
	if len(s.pending) > 0 {
		return s.shift(), nil
	}
	for {
		frame, err := s.reader.Next(ctx)
		if err != nil {
			if err == io.EOF && s.started && !s.completed {
				deliverycompat.EmitTerminalUsagePresence(ctx, s.sink, s.exchangeID, false)
				s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseID, Payload: canonical.ErrorPayload{Code: "stream_unexpected_eof", Message: "output stream ended before completed"}})
				s.blocks = map[int]*streamContentBlock{}
				s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusError)
				s.completed = true
				if len(s.pending) > 0 {
					return s.shift(), nil
				}
			}
			return canonical.Event{}, err
		}
		if strings.TrimSpace(frame.Data) == "" || frame.Event == "ping" { // swobu:io-string source=boundary
			continue
		}
		frameUsage := core.ExtractTokenUsage([]byte(frame.Data), tokenUsagePathSpec)
		if !frameUsage.IsZero() {
			s.latestUsage = frameUsage
			_, inputPresent := frameUsage.InputTokens()
			openaiwire.EmitUsageDecision(ctx, s.sink, s.exchangeID, inputPresent, compat.ResponseUsageInputTokens, compat.Subject("wire:/usage/input_tokens"))
			_, outputPresent := frameUsage.OutputTokens()
			openaiwire.EmitUsageDecision(ctx, s.sink, s.exchangeID, outputPresent, compat.ResponseUsageOutputTokens, compat.Subject("wire:/usage/output_tokens"))
			_, cacheReadPresent := frameUsage.CacheReadTokens()
			openaiwire.EmitUsageDecision(ctx, s.sink, s.exchangeID, cacheReadPresent, compat.ResponseUsageCacheReadTokens, compat.Subject("wire:/usage/cache_read_tokens"))
			_, cacheWritePresent := frameUsage.CacheWriteTokens()
			openaiwire.EmitUsageDecision(ctx, s.sink, s.exchangeID, cacheWritePresent, compat.ResponseUsageCacheWriteTokens, compat.Subject("wire:/usage/cache_write_tokens"))
		}
		var envelope streamEnvelope
		if err := json.Unmarshal([]byte(frame.Data), &envelope); err != nil {
			return canonical.Event{}, canonical.InternalError("messages stream frame is invalid JSON")
		}
		if err := s.handleFrame(ctx, envelope.Type, frame.Data); err != nil {
			return canonical.Event{}, err
		}
		if len(s.pending) > 0 {
			return s.shift(), nil
		}
	}
}

func (s *messagesEventReader) handleFrame(ctx context.Context, frameType string, raw string) error {
	normalizedFrameType := strings.TrimSpace(frameType) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
	switch normalizedFrameType {
	case "message_start":
		return s.handleMessageStart(raw)
	case "content_block_start":
		return s.handleContentBlockStart(raw)
	case "content_block_delta":
		return s.handleContentBlockDelta(raw)
	case "content_block_stop":
		return s.handleContentBlockStop(raw)
	case "message_delta":
		return s.handleMessageDelta(raw)
	case "message_stop":
		s.handleMessageStop(ctx)
		return nil
	case "ping":
		return nil
	default:
		return canonical.InternalError("messages stream frame type is unsupported")
	}
}

func (s *messagesEventReader) handleMessageStart(raw string) error {
	var payload messageStartFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return canonical.InternalError("messages stream message_start frame is invalid")
	}
	s.started = true
	s.resultID = payload.Message.ID
	s.model = payload.Message.Model
	s.enqueueEnvelopeStart(s.responseID, "", canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse, Model: s.model})
	s.enqueue(canonical.Event{Kind: canonical.EventResponseIdentity, EnvID: s.responseID, Payload: canonical.ResponseIdentityPayload{Response: canonical.ResponseRef{}}})
	return nil
}

func (s *messagesEventReader) handleContentBlockStart(raw string) error {
	var payload contentBlockStartFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return canonical.InternalError("messages stream content_block_start frame is invalid")
	}
	block := &streamContentBlock{}
	contentBlockType := strings.TrimSpace(payload.ContentBlock.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
	switch contentBlockType {
	case "text":
		block.ItemKind = canonical.ItemKindMessage
		block.citations = append(block.citations, payload.ContentBlock.Citations...)
		start, err := canonical.NewMessageStart(canonical.MessageRoleAssistant)
		if err != nil {
			return err
		}
		s.enqueue(canonical.Event{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: uint32(payload.Index)}, Payload: start}})
		s.enqueue(canonical.Event{Kind: canonical.EventContentStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: uint32(payload.Index)}, Payload: canonical.NewMessageContentStart(canonical.PartKindText)}})
	case "tool_use":
		block.ItemKind = canonical.ItemKindToolCall
		callID, err := canonical.NewToolCallID(payload.ContentBlock.ID)
		if err != nil {
			return canonical.InternalError("messages stream tool_use is missing id")
		}
		resolved, _, err := canonical.ResolveToolDeclarationByName(s.request.Tools(), payload.ContentBlock.Name, canonical.ToolTypeFunction)
		if err != nil {
			return canonical.InternalError("messages stream tool_use references an unknown or ambiguous tool")
		}
		block.CallID = callID
		block.Tool = resolved.Key()
		block.initialInput = append(json.RawMessage(nil), payload.ContentBlock.Input...)
		start, err := canonical.NewToolCallStart(callID, block.Tool)
		if err != nil {
			return err
		}
		s.enqueue(canonical.Event{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: uint32(payload.Index)}, Payload: start}})
	case "thinking":
		block.ItemKind = canonical.ItemKindReasoning
		block.reasoningType = "thinking"
		block.signature.WriteString(payload.ContentBlock.Signature)
		if payload.ContentBlock.Thinking != "" {
			block.text.WriteString(payload.ContentBlock.Thinking)
		}
	case "redacted_thinking":
		block.ItemKind = canonical.ItemKindReasoning
		block.reasoningType = "redacted_thinking"
		block.data = payload.ContentBlock.Data
	case "server_tool_use":
		if strings.TrimSpace(payload.ContentBlock.Name) != "web_search" { // swobu:io-string source=provider-wire
			return canonical.NotImplemented("Swobu has no canonical projection for this streamed Messages server-tool type")
		}
		block.ItemKind = canonical.ItemKindToolCall
		callID, err := canonical.NewToolCallID(payload.ContentBlock.ID)
		if err != nil {
			return canonical.InternalError("messages streamed web-search call is missing id")
		}
		block.CallID, block.Tool = callID, canonical.WebSearchToolKey()
		block.initialInput = append(json.RawMessage(nil), payload.ContentBlock.Input...)
		start, err := canonical.NewToolCallStart(callID, block.Tool)
		if err != nil {
			return err
		}
		s.enqueue(canonical.Event{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: uint32(payload.Index)}, Payload: start}})
	case "web_search_tool_result":
		block.ItemKind = canonical.ItemKindToolResult
		callID, err := canonical.NewToolCallID(payload.ContentBlock.ToolUseID)
		if err != nil {
			return canonical.InternalError("messages streamed web-search result is missing tool_use_id")
		}
		block.CallID = callID
		block.searchResult = append(json.RawMessage(nil), payload.ContentBlock.Content...)
		block.searchError = payload.ContentBlock.IsError
	default:
		return canonical.InternalError("messages stream content block type is unsupported")
	}
	s.blocks[payload.Index] = block
	return nil
}

func (s *messagesEventReader) handleContentBlockDelta(raw string) error {
	var payload contentBlockDeltaFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return canonical.InternalError("messages stream content_block_delta frame is invalid")
	}
	block, ok := s.blocks[payload.Index]
	if !ok {
		return nil
	}
	deltaType := strings.TrimSpace(payload.Delta.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
	switch deltaType {
	case "text_delta":
		block.text.WriteString(payload.Delta.Text)
		s.enqueue(canonical.Event{
			Kind:    canonical.EventTextDelta,
			EnvID:   s.blockEnvID(payload.Index),
			Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: uint32(payload.Index)}, Payload: canonical.TextDeltaPayload{Text: payload.Delta.Text}},
		})
	case "input_json_delta":
		if len(block.initialInput) > 0 && string(block.initialInput) != "{}" {
			return canonical.InternalError("messages stream tool_use mixed initial input with argument deltas")
		}
		block.args.WriteString(payload.Delta.PartialJSON)
		if block.Tool.Kind() != canonical.ToolKindWebSearch {
			s.enqueue(canonical.Event{Kind: canonical.EventArgsDelta, EnvID: s.blockEnvID(payload.Index), Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: uint32(payload.Index)}, Payload: canonical.ArgsDeltaPayload{Args: payload.Delta.PartialJSON}}})
		}
	case "thinking_delta":
		text := payload.Delta.Thinking
		if text == "" {
			text = payload.Delta.Text
		}
		block.text.WriteString(text)
	case "signature_delta":
		block.signature.WriteString(payload.Delta.Signature)
	case "citations_delta", "citation_delta":
		block.citations = append(block.citations, payload.Delta.Citation)
	default:
		return canonical.InternalError("messages stream delta type is unsupported")
	}
	s.blocks[payload.Index] = block
	return nil
}

func (s *messagesEventReader) handleContentBlockStop(raw string) error {
	var payload contentBlockStopFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return canonical.InternalError("messages stream content_block_stop frame is invalid")
	}
	block, ok := s.blocks[payload.Index]
	if !ok {
		return nil
	}
	var item canonical.CanonicalItem
	var err error
	switch block.ItemKind {
	case canonical.ItemKindMessage:
		part, partErr := decodeMessagesCitedText(block.text.String(), block.citations)
		if partErr != nil {
			return partErr
		}
		item, err = canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{part})
	case canonical.ItemKindToolCall:
		if block.Tool.Kind() == canonical.ToolKindWebSearch {
			raw := block.initialInput
			if block.args.Len() > 0 {
				raw = json.RawMessage(block.args.String())
			}
			item, err = decodeMessagesWebSearchCall(block.CallID.String(), raw)
			break
		}
		if block.args.Len() == 0 {
			raw := block.initialInput
			if len(raw) == 0 {
				raw = json.RawMessage(`{}`)
			}
			block.args.Write(raw)
			s.enqueue(canonical.Event{
				Kind:    canonical.EventArgsDelta,
				EnvID:   s.blockEnvID(payload.Index),
				Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: uint32(payload.Index)}, Payload: canonical.ArgsDeltaPayload{Args: string(raw)}},
			})
		}
		object, parseErr := canonical.ParseJSONObject([]byte(block.args.String()))
		if parseErr != nil {
			return canonical.InternalError("messages streamed tool_use input is invalid")
		}
		item, err = canonical.NewToolCallItem(block.CallID, block.Tool, canonical.NewJSONObjectToolInput(object))
	case canonical.ItemKindToolResult:
		item, err = decodeMessagesWebSearchResult(block.CallID.String(), block.searchResult, block.searchError)
	case canonical.ItemKindReasoning:
		wireBlock := contentID{Type: block.reasoningType}
		if block.reasoningType == "redacted_thinking" {
			wireBlock.Data = block.data
			raw, marshalErr := json.Marshal(wireBlock)
			if marshalErr != nil {
				return canonical.InternalError("messages streamed redacted thinking block is invalid")
			}
			opaque, refineErr := canonical.NewMessagesOpaqueThinking(raw)
			if refineErr != nil {
				return canonical.InternalError("messages streamed redacted thinking data is invalid")
			}
			item, err = canonical.NewReasoningItem(nil, opaque)
		} else {
			text := block.text.String()
			wireBlock.Thinking = &text
			wireBlock.Signature = block.signature.String()
			raw, marshalErr := json.Marshal(wireBlock)
			if marshalErr != nil {
				return canonical.InternalError("messages streamed thinking block is invalid")
			}
			opaque, refineErr := canonical.NewMessagesOpaqueThinking(raw)
			if refineErr != nil {
				return canonical.InternalError("messages streamed thinking signature is invalid")
			}
			var parts []canonical.ReasoningPart
			if block.text.Len() > 0 {
				part, partErr := canonical.NewReasoningPart(messagesResponseReasoningKind(s.request), block.text.String())
				if partErr != nil {
					return canonical.InternalError("messages streamed thinking text is invalid")
				}
				parts = []canonical.ReasoningPart{part}
			}
			item, err = canonical.NewReasoningItem(parts, opaque)
		}
	default:
		return canonical.InternalError("messages streamed content block kind is invalid")
	}
	if err != nil {
		return canonical.InternalError("messages streamed content block is invalid")
	}
	s.enqueue(canonical.Event{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: uint32(payload.Index)}, Payload: canonical.ItemCompletedPayload{Item: item}}})
	delete(s.blocks, payload.Index)
	return nil
}

func (s *messagesEventReader) handleMessageDelta(raw string) error {
	var payload messageDeltaFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return canonical.InternalError("messages stream message_delta frame is invalid")
	}
	s.finishReason = strings.TrimSpace(payload.Delta.StopReason) // swobu:io-string source=boundary
	return nil
}

func (s *messagesEventReader) handleMessageStop(ctx context.Context) {
	s.completed = true
	finishReason := s.finishReason
	if finishReason == "" {
		finishReason = "completed"
	}
	deliverycompat.EmitTerminalUsagePresence(ctx, s.sink, s.exchangeID, !s.latestUsage.IsZero())
	s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseID, Payload: canonical.UsagePayload{Usage: s.latestUsage}})
	s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseID, Payload: canonical.FinishPayload{Reason: finishReason}})
	s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
}

func (s *messagesEventReader) Close(context.Context) error {
	return s.reader.Close()
}

func (s *messagesEventReader) shift() canonical.Event {
	event := s.pending[0]
	s.pending = s.pending[1:]
	return event
}

func (s *messagesEventReader) nextSeq() int64 {
	s.seq++
	return s.seq
}

func (s *messagesEventReader) enqueue(ev canonical.Event) {
	ev.ExchangeID = s.exchangeID
	ev.Seq = s.nextSeq()
	ev.Time = time.Now().UTC()
	s.pending = append(s.pending, ev)
}

func (s *messagesEventReader) enqueueEnvelopeStart(id canonical.EnvelopeID, parent canonical.EnvelopeID, payload canonical.EnvelopeStartPayload, meta ...canonical.EventMetadataFields) {
	ev := canonical.Event{Kind: canonical.EventEnvelopeStart, EnvID: id, ParentID: parent, Payload: payload}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	s.enqueue(ev)
}

func (s *messagesEventReader) enqueueEnvelopeEnd(id canonical.EnvelopeID, kind canonical.EnvelopeKind, status canonical.EnvelopeStatus) {
	s.enqueue(canonical.Event{Kind: canonical.EventEnvelopeEnd, EnvID: id, Payload: canonical.EnvelopeEndPayload{Kind: kind, Status: status}})
}

func (s *messagesEventReader) blockEnvID(index int) canonical.EnvelopeID {
	return canonical.EnvelopeID(fmt.Sprintf("%s:item:%d", s.responseID, index))
}
