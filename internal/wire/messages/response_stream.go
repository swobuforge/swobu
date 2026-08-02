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
	"github.com/swobuforge/swobu/internal/wire"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

// DecodeResponseStream returns canonical envelope events directly for messages streams.
func decodeResponseStream(request canonical.CanonicalRequest, names wire.ToolNames, stream carrier.ByteStream, exchangeID string, _ *[]compat.Change) *messagesEventReader {
	reader := &messagesEventReader{
		exchangeID:     exchangeID,
		responseID:     canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		reader:         core.NewSSEReader(stream.Body),
		blocks:         map[int]*streamContentBlock{},
		resolvedBlocks: map[int]struct{}{},
		unknownEvents:  map[string]struct{}{},
		latestUsage:    canonical.NewUnknownTokenUsage(),
		request:        request.Clone(),
		toolNames:      names,
	}
	reader.changeLog = &reader.changes
	return reader
}

type messagesResponseLifecycle uint8

const (
	messagesResponseUnseen messagesResponseLifecycle = iota
	messagesResponseStarted
	messagesResponseStopped
)

type messagesEventReader struct {
	exchangeID         string
	responseID         canonical.EnvelopeID
	changeLog          *[]compat.Change
	changes            []compat.Change
	reader             *core.SSEReaderCloser
	resultID           string
	model              string
	finishReason       string
	lifecycle          messagesResponseLifecycle
	pending            canonical.EventSequence
	blocks             map[int]*streamContentBlock
	resolvedBlocks     map[int]struct{}
	unknownEvents      map[string]struct{}
	latestUsage        canonical.TokenUsage
	nextOrdinal        uint32
	nextBlockIndex     int
	frameIndex         int
	erasedBlock        bool
	completedItems     uint32
	completedToolCalls uint32
	seq                int64
	request            canonical.CanonicalRequest
	toolNames          wire.ToolNames
}

func (s *messagesEventReader) Changes() []compat.Change {
	return compat.CloneChanges(s.changes)
}

type streamContentBlock struct {
	ItemKind             canonical.ItemKind
	Ordinal              uint32
	CallID               canonical.ToolCallID
	Tool                 canonical.ToolKey
	text                 strings.Builder
	args                 strings.Builder
	initialInput         json.RawMessage
	reasoningType        string
	signature            strings.Builder
	data                 string
	searchResult         json.RawMessage
	searchError          bool
	citations            []messagesCitationDTO
	unknownDeltaRecorded bool
}

type streamEnvelope struct {
	Type  string `json:"type"`
	Index *int   `json:"index"`
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
			if err == io.EOF && s.lifecycle == messagesResponseUnseen {
				return canonical.Event{}, canonical.NewBackendError("messages", 0, "messages stream ended before message_start", "")
			}
			if err == io.EOF && s.lifecycle == messagesResponseStarted {
				s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseID, Payload: canonical.ErrorPayload{Code: "stream_unexpected_eof", Message: "output stream ended before completed"}})
				s.blocks = map[int]*streamContentBlock{}
				s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusError)
				s.lifecycle = messagesResponseStopped
				if len(s.pending) > 0 {
					return s.shift(), nil
				}
			}
			return canonical.Event{}, err
		}
		if s.lifecycle == messagesResponseStopped {
			return canonical.Event{}, canonical.NewBackendError("messages", 0, "messages stream frame arrived after message_stop", "")
		}
		if strings.TrimSpace(frame.Data) == "" || frame.Event == "ping" { // swobu:io-string source=boundary
			continue
		}
		frameUsage := core.ExtractTokenUsage([]byte(frame.Data), tokenUsagePathSpec)
		if !frameUsage.IsZero() {
			s.latestUsage = mergeMessagesCumulativeUsage(s.latestUsage, frameUsage)
		}
		var envelope streamEnvelope
		if err := json.Unmarshal([]byte(frame.Data), &envelope); err != nil {
			return canonical.Event{}, canonical.InternalError("messages stream frame is invalid JSON")
		}
		currentFrame := s.frameIndex
		s.frameIndex++
		if err := s.handleFrame(ctx, envelope, frame.Data, currentFrame); err != nil {
			return canonical.Event{}, err
		}
		if len(s.pending) > 0 {
			return s.shift(), nil
		}
	}
}

func mergeMessagesCumulativeUsage(previous canonical.TokenUsage, current canonical.TokenUsage) canonical.TokenUsage {
	input := cumulativeUsageField(previous.InputTokens, current.InputTokens)
	output := cumulativeUsageField(previous.OutputTokens, current.OutputTokens)
	cacheRead := cumulativeUsageField(previous.CacheReadTokens, current.CacheReadTokens)
	cacheWrite := cumulativeUsageField(previous.CacheWriteTokens, current.CacheWriteTokens)
	merged, _ := canonical.NewTokenUsage(canonical.TokenUsageParams{
		InputTokens: input, OutputTokens: output,
		CacheReadTokens: cacheRead, CacheWriteTokens: cacheWrite,
	})
	return merged
}

func cumulativeUsageField(previous func() (int, bool), current func() (int, bool)) *int {
	if value, ok := current(); ok {
		return &value
	}
	if value, ok := previous(); ok {
		return &value
	}
	return nil
}

func (s *messagesEventReader) handleFrame(ctx context.Context, envelope streamEnvelope, raw string, frameIndex int) error {
	normalizedFrameType := strings.TrimSpace(envelope.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
	if normalizedFrameType == "" {
		return canonical.NewBackendError("messages", 0, "messages stream frame is missing type", "")
	}
	if s.lifecycle == messagesResponseStopped {
		return canonical.NewBackendError("messages", 0, "messages stream frame arrived after message_stop", "")
	}
	if s.lifecycle == messagesResponseUnseen && normalizedFrameType != "message_start" && normalizedFrameType != "ping" {
		return canonical.NewBackendError("messages", 0, "messages stream frame arrived before message_start", "")
	}
	if s.lifecycle == messagesResponseStarted && normalizedFrameType == "message_start" {
		return canonical.NewBackendError("messages", 0, "messages stream received a second message_start", "")
	}
	switch normalizedFrameType {
	case "message_start":
		if err := s.handleMessageStart(raw); err != nil {
			return err
		}
		s.lifecycle = messagesResponseStarted
		return nil
	case "content_block_start":
		return s.handleContentBlockStart(raw)
	case "content_block_delta":
		return s.handleContentBlockDelta(raw)
	case "content_block_stop":
		return s.handleContentBlockStop(raw)
	case "message_delta":
		return s.handleMessageDelta(raw)
	case "message_stop":
		if err := s.handleMessageStop(ctx); err != nil {
			return err
		}
		s.lifecycle = messagesResponseStopped
		return nil
	case "ping":
		return nil
	default:
		key := messagesUnknownEventDecisionKey(normalizedFrameType, envelope.Index)
		if _, recorded := s.unknownEvents[key]; recorded {
			return nil
		}
		s.unknownEvents[key] = struct{}{}
		return nil
	}
}

func messagesUnknownEventDecisionKey(frameType string, blockIndex *int) string {
	if blockIndex != nil {
		return frameType + "\x00" + fmt.Sprintf("block:%d", *blockIndex)
	}
	return frameType
}

func (s *messagesEventReader) handleMessageStart(raw string) error {
	var payload messageStartFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return canonical.InternalError("messages stream message_start frame is invalid")
	}
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
	if payload.Index != s.nextBlockIndex {
		return canonical.NewBackendError("messages", 0, "messages content block starts are out of provider index order", "")
	}
	if _, active := s.blocks[payload.Index]; active {
		return canonical.NewBackendError("", 0, "messages content block index received a second start", "")
	}
	if _, resolved := s.resolvedBlocks[payload.Index]; resolved {
		return canonical.NewBackendError("", 0, "messages content block index was reused after stop", "")
	}
	block := &streamContentBlock{Ordinal: s.nextOrdinal}
	contentBlockType := strings.TrimSpace(payload.ContentBlock.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
	if contentBlockType == "" {
		return canonical.NewBackendError("messages", 0, "messages content block start is missing type", "")
	}
	switch contentBlockType {
	case "text":
		block.ItemKind = canonical.ItemKindMessage
		block.citations = append(block.citations, payload.ContentBlock.Citations...)
		start, err := canonical.NewMessageStart(canonical.MessageRoleAssistant)
		if err != nil {
			return err
		}
		s.enqueue(canonical.Event{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: block.Ordinal}, Payload: start}})
		s.enqueue(canonical.Event{Kind: canonical.EventContentStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: block.Ordinal}, Payload: canonical.NewMessageContentStart(canonical.PartKindText)}})
	case "tool_use":
		block.ItemKind = canonical.ItemKindToolCall
		callID, err := canonical.NewToolCallID(payload.ContentBlock.ID)
		if err != nil {
			return canonical.InternalError("messages stream tool_use is missing id")
		}
		environment, err := canonical.EffectiveTools(s.request)
		if err != nil {
			return canonical.InternalError("messages stream tool environment is ambiguous")
		}
		key, err := wire.DecodeToolKey(s.toolNames, environment, canonical.ToolKindFunction, payload.ContentBlock.Name)
		if err != nil {
			return canonical.InternalError("messages stream tool_use references an unknown or ambiguous tool")
		}
		block.CallID = callID
		block.Tool = key
		block.initialInput = append(json.RawMessage(nil), payload.ContentBlock.Input...)
		start, err := canonical.NewToolCallStart(callID, block.Tool)
		if err != nil {
			return err
		}
		s.enqueue(canonical.Event{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: block.Ordinal}, Payload: start}})
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
			s.erasedBlock = true
			if err := appendMessagesOccurrenceChange(s.changeLog, s.exchangeID, canonical.ResponseItemsKind, compat.Omission, canonical.ResponseItemOccurrence(block.Ordinal)); err != nil {
				return err
			}
			break
		}
		block.ItemKind = canonical.ItemKindToolCall
		callID, err := canonical.NewToolCallID(payload.ContentBlock.ID)
		if err != nil {
			return canonical.NewBackendError("messages", 0, "messages streamed web-search call is missing id", "")
		}
		block.CallID, block.Tool = callID, canonical.WebSearchToolKey()
		block.initialInput = append(json.RawMessage(nil), payload.ContentBlock.Input...)
		start, err := canonical.NewToolCallStart(callID, block.Tool)
		if err != nil {
			return err
		}
		s.enqueue(canonical.Event{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: block.Ordinal}, Payload: start}})
	case "web_search_tool_result":
		block.ItemKind = canonical.ItemKindToolResult
		callID, err := canonical.NewToolCallID(payload.ContentBlock.ToolUseID)
		if err != nil {
			return canonical.NewBackendError("messages", 0, "messages streamed web-search result is missing tool_use_id", "")
		}
		block.CallID = callID
		block.searchResult = append(json.RawMessage(nil), payload.ContentBlock.Content...)
		block.searchError = payload.ContentBlock.IsError
	default:
		// Retain the index until content_block_stop so deltas for an unknown
		// additive block cannot affect known siblings.
		s.erasedBlock = true
		if err := appendMessagesOccurrenceChange(s.changeLog, s.exchangeID, canonical.ResponseItemsKind, compat.Omission, canonical.ResponseItemOccurrence(block.Ordinal)); err != nil {
			return err
		}
	}
	if block.ItemKind != "" {
		s.nextOrdinal++
	}
	s.blocks[payload.Index] = block
	s.nextBlockIndex++
	return nil
}

func (s *messagesEventReader) handleContentBlockDelta(raw string) error {
	var payload contentBlockDeltaFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return canonical.InternalError("messages stream content_block_delta frame is invalid")
	}
	block, ok := s.blocks[payload.Index]
	if !ok {
		if _, resolved := s.resolvedBlocks[payload.Index]; resolved {
			return canonical.NewBackendError("", 0, "messages content block continued after stop", "")
		}
		return canonical.NewBackendError("messages", 0, "messages content block delta arrived before start", "")
	}
	deltaType := strings.TrimSpace(payload.Delta.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
	if deltaType == "" {
		return canonical.NewBackendError("messages", 0, "messages content block delta is missing type", "")
	}
	if block.ItemKind == "" {
		return nil
	}
	if isKnownMessagesDeltaType(deltaType) && !messagesBlockAdmitsDelta(block, deltaType) {
		// Validate the composed identity before mutating progressive state or
		// enqueueing an event. A recognized delta on the wrong admitted block is
		// a provider contradiction, not additive novelty.
		return canonical.NewBackendError("messages", 0, "messages content block delta type is incompatible with block type", "")
	}
	switch deltaType {
	case "text_delta":
		block.text.WriteString(payload.Delta.Text)
		s.enqueue(canonical.Event{
			Kind:    canonical.EventTextDelta,
			Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: block.Ordinal}, Payload: canonical.TextDeltaPayload{Text: payload.Delta.Text}},
		})
	case "input_json_delta":
		if len(block.initialInput) > 0 && string(block.initialInput) != "{}" {
			return canonical.NewBackendError("messages", 0, "messages stream tool_use mixed initial input with argument deltas", "")
		}
		block.args.WriteString(payload.Delta.PartialJSON)
		if block.Tool.Kind() != canonical.ToolKindWebSearch {
			s.enqueue(canonical.Event{Kind: canonical.EventArgsDelta, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: block.Ordinal}, Payload: canonical.ArgsDeltaPayload{Args: payload.Delta.PartialJSON}}})
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
		if block.unknownDeltaRecorded {
			return nil
		}
		block.unknownDeltaRecorded = true
		return appendMessagesOccurrenceChange(s.changeLog, s.exchangeID, canonical.ResponseItemsKind, compat.Omission, canonical.ResponseItemOccurrence(block.Ordinal))
	}
	s.blocks[payload.Index] = block
	return nil
}

// messagesBlockAdmitsDelta is the closed Messages progressive-content grammar.
// Block identity is frozen at start; recognized deltas must compose with that
// identity before they may mutate state or emit canonical events.
func messagesBlockAdmitsDelta(block *streamContentBlock, deltaType string) bool {
	switch block.ItemKind {
	case canonical.ItemKindMessage:
		return deltaType == "text_delta" ||
			deltaType == "citations_delta" ||
			deltaType == "citation_delta"
	case canonical.ItemKindToolCall:
		return deltaType == "input_json_delta"
	case canonical.ItemKindReasoning:
		return block.reasoningType == "thinking" &&
			(deltaType == "thinking_delta" || deltaType == "signature_delta")
	default:
		return false
	}
}

func isKnownMessagesDeltaType(deltaType string) bool {
	switch deltaType {
	case "text_delta", "input_json_delta", "thinking_delta",
		"signature_delta", "citations_delta", "citation_delta":
		return true
	default:
		return false
	}
}

func (s *messagesEventReader) handleContentBlockStop(raw string) error {
	var payload contentBlockStopFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return canonical.InternalError("messages stream content_block_stop frame is invalid")
	}
	block, ok := s.blocks[payload.Index]
	if !ok {
		if _, resolved := s.resolvedBlocks[payload.Index]; resolved {
			return canonical.NewBackendError("", 0, "messages content block received a second stop", "")
		}
		return canonical.NewBackendError("messages", 0, "messages content block stop arrived before start", "")
	}
	if block.ItemKind == "" {
		delete(s.blocks, payload.Index)
		s.resolvedBlocks[payload.Index] = struct{}{}
		return nil
	}
	var item canonical.CanonicalItem
	var err error
	switch block.ItemKind {
	case canonical.ItemKindMessage:
		part, partErr := decodeMessagesCitedText(block.text.String(), block.citations, messagesProjectionEvidence{
			feature: canonical.ResponseItemsKind, changeLog: s.changeLog, exchangeID: s.exchangeID,
			occurrence: canonical.ResponseItemOccurrence(block.Ordinal),
		})
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
			item, err = decodeMessagesWebSearchCall(block.CallID.String(), raw, messagesProjectionEvidence{
				feature: canonical.ResponseItemsKind, changeLog: s.changeLog, exchangeID: s.exchangeID,
				occurrence: canonical.ResponseItemOccurrence(block.Ordinal),
			})
			if err != nil {
				return err
			}
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
				Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: block.Ordinal}, Payload: canonical.ArgsDeltaPayload{Args: string(raw)}},
			})
		}
		object, parseErr := canonical.ParseJSONObject([]byte(block.args.String()))
		if parseErr != nil {
			return canonical.InternalError("messages streamed tool_use input is invalid")
		}
		item, err = canonical.NewToolCallItem(block.CallID, block.Tool, canonical.NewJSONObjectToolInput(object))
	case canonical.ItemKindToolResult:
		item, err = decodeMessagesWebSearchResult(block.CallID.String(), block.searchResult, block.searchError, messagesProjectionEvidence{
			feature: canonical.ResponseItemsKind, changeLog: s.changeLog, exchangeID: s.exchangeID,
			occurrence: canonical.ResponseItemOccurrence(block.Ordinal),
		})
		if err != nil {
			return err
		}
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
	s.enqueue(canonical.Event{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: block.Ordinal}, Payload: canonical.ItemCompletedPayload{Item: item}}})
	s.completedItems++
	if _, ok := item.ToolCall(); ok {
		s.completedToolCalls++
	}
	delete(s.blocks, payload.Index)
	s.resolvedBlocks[payload.Index] = struct{}{}
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

func (s *messagesEventReader) handleMessageStop(ctx context.Context) error {
	if len(s.blocks) > 0 {
		return canonical.NewBackendError("", 0, "messages response ended with incomplete content blocks", "")
	}
	if s.erasedBlock && s.completedItems == 0 {
		return canonical.NewBackendError("", 0, "backend produced no usable canonical output", "")
	}
	if s.finishReason == "tool_use" && s.completedToolCalls == 0 {
		return canonical.NewBackendError("", 0, "messages stop reason requires a surviving tool call", "")
	}
	finishReason := s.finishReason
	if finishReason == "" {
		finishReason = "completed"
	}
	s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseID, Payload: canonical.UsagePayload{Usage: s.latestUsage}})
	s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseID, Payload: canonical.FinishPayload{Completion: messagesCompletion(finishReason)}})
	s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
	return nil
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
