package messages

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	deliverycompat "github.com/swobuforge/swobu/internal/adapters/wire/families/deliverycompat"
	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
)

type bufferedResponseBody struct {
	ID         string                     `json:"id"`
	Model      string                     `json:"model"`
	Content    []bufferedContentBlockBody `json:"content"`
	StopReason string                     `json:"stop_reason"`
}

type bufferedContentBlockBody struct {
	Type      string         `json:"type"`
	Text      string         `json:"text"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
	ToolUseID string         `json:"tool_use_id"`
}

var tokenUsagePathSpec = core.TokenUsagePathSpec{
	InputPaths: [][]string{
		{"usage", "input_tokens"},
		{"usage", "prompt_tokens"},
		{"usageMetadata", "promptTokenCount"},
		{"usage", "inputTokens"},
	},
	OutputPaths: [][]string{
		{"usage", "output_tokens"},
		{"usage", "completion_tokens"},
		{"usageMetadata", "candidatesTokenCount"},
		{"usage", "outputTokens"},
	},
	CacheReadPaths: [][]string{
		{"usage", "cache_read_input_tokens"},
		{"usage", "input_tokens_details", "cached_tokens"},
		{"usage", "prompt_tokens_details", "cached_tokens"},
		{"usageMetadata", "cachedContentTokenCount"},
		{"usage", "cacheReadInputTokens"},
	},
	CacheWritePaths: [][]string{
		{"usage", "cache_creation_input_tokens"},
		{"usage", "input_tokens_details", "cache_write_tokens"},
		{"usage", "prompt_tokens_details", "cache_write_tokens"},
		{"usage", "cacheWriteInputTokens"},
	},
}

func decodeResponseBuffered(ctx context.Context, raw []byte, exchangeID string, sink effect.Sink) (canonical.EventReader, error) {
	var dto bufferedResponseBody
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, canonical.InternalError("messages response is invalid JSON")
	}
	usage := core.ExtractTokenUsage(raw, tokenUsagePathSpec)
	items := make([]canonical.CanonicalItem, 0, len(dto.Content))
	for i, block := range dto.Content {
		blockType := strings.TrimSpace(block.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
		switch blockType {
		case "text":
			items = append(items, canonical.NewTextOutputItem("text_"+strconv.Itoa(i), block.Text))
		case "tool_use":
			itemID := strings.TrimSpace(block.ID) // swobu:io-string source=boundary
			if itemID == "" {
				itemID = "tool_" + strconv.Itoa(i)
			}
			args, marshalErr := json.Marshal(block.Input)
			if marshalErr != nil {
				return nil, canonical.InternalError("messages response tool_use input is invalid JSON object")
			}
			items = append(items, canonical.NewToolUseOutputItem(itemID, strings.TrimSpace(block.ID), strings.TrimSpace(block.Name), canonical.NewToolArgumentsObject(string(args)))) // swobu:io-string source=boundary
		default:
			return nil, canonical.InternalError("messages response content block is unsupported")
		}
	}
	emitUsageInputTokensDecision(ctx, sink, exchangeID, usage)
	emitUsageOutputTokensDecision(ctx, sink, exchangeID, usage)
	emitUsageCacheReadTokensDecision(ctx, sink, exchangeID, usage)
	emitUsageCacheWriteTokensDecision(ctx, sink, exchangeID, usage)
	return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		exchangeID,
		dto.ID,
		dto.Model,
		items,
		dto.StopReason,
		usage,
	)), nil
}

func emitUsageInputTokensDecision(ctx context.Context, sink effect.Sink, exchangeID string, usage canonical.TokenUsage) {
	if sink == nil {
		return
	}
	if _, ok := usage.InputTokens(); !ok {
		return
	}
	_ = sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.Compatibility{
			Feature: compat.UsageInputTokens,
			Outcome: compat.Exact,
			Subject: compat.Subject("wire:/usage/input_tokens"),
		},
	})
}

func emitUsageOutputTokensDecision(ctx context.Context, sink effect.Sink, exchangeID string, usage canonical.TokenUsage) {
	if sink == nil {
		return
	}
	if _, ok := usage.OutputTokens(); !ok {
		return
	}
	_ = sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.Compatibility{
			Feature: compat.UsageOutputTokens,
			Outcome: compat.Exact,
			Subject: compat.Subject("wire:/usage/output_tokens"),
		},
	})
}

func emitUsageCacheReadTokensDecision(ctx context.Context, sink effect.Sink, exchangeID string, usage canonical.TokenUsage) {
	if sink == nil {
		return
	}
	if _, ok := usage.CacheReadTokens(); !ok {
		return
	}
	_ = sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.Compatibility{
			Feature: compat.UsageCacheReadTokens,
			Outcome: compat.Exact,
			Subject: compat.Subject("wire:/usage/cache_read_tokens"),
		},
	})
}

func emitUsageCacheWriteTokensDecision(ctx context.Context, sink effect.Sink, exchangeID string, usage canonical.TokenUsage) {
	if sink == nil {
		return
	}
	if _, ok := usage.CacheWriteTokens(); !ok {
		return
	}
	_ = sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.Compatibility{
			Feature: compat.UsageCacheWriteTokens,
			Outcome: compat.Exact,
			Subject: compat.Subject("wire:/usage/cache_write_tokens"),
		},
	})
}

// DecodeResponseStream returns canonical envelope events directly for messages streams.
func decodeResponseStream(stream carrier.WireStream, exchangeID string, sink effect.Sink) canonical.EventReader {
	recording := &effect.RecordingSink{Delegate: sink}
	return &messagesEventReader{
		exchangeID:  exchangeID,
		responseID:  canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		sink:        recording,
		recording:   recording,
		reader:      core.NewSSEReader(carrier.ReadCloserFromFrameReader(stream.Frames)),
		blocks:      map[int]streamContentBlock{},
		latestUsage: canonical.NewUnknownTokenUsage(),
	}
}

type messagesEventReader struct {
	exchangeID   string
	responseID   canonical.EnvelopeID
	sink         effect.Sink
	recording    *effect.RecordingSink
	reader       *core.SSEReaderCloser
	resultID     string
	model        string
	finishReason string
	started      bool
	pending      canonical.EventSequence
	blocks       map[int]streamContentBlock
	latestUsage  canonical.TokenUsage
	seq          int64
	completed    bool
}

func (s *messagesEventReader) Effects() []effect.Effect {
	if s.recording == nil {
		return nil
	}
	return append([]effect.Effect(nil), s.recording.Effects...)
}

type streamContentBlock struct {
	ItemID    string
	ItemKind  canonical.ItemKind
	ToolUseID string
	Name      string
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
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
}

type contentBlockDeltaFrame struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
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
		frame, err := s.reader.Next()
		if err != nil {
			if err == io.EOF && s.started && !s.completed {
				deliverycompat.EmitTerminalEventDecision(ctx, s.sink, s.exchangeID, false)
				s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseID, Payload: canonical.ErrorPayload{Code: "stream_unexpected_eof", Message: "output stream ended before completed"}})
				for idx, block := range s.blocks {
					s.enqueueEnvelopeEnd(s.blockEnvID(idx), s.blockKind(block), canonical.EnvelopeStatusError)
				}
				s.blocks = map[int]streamContentBlock{}
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
			emitUsageInputTokensDecision(ctx, s.sink, s.exchangeID, frameUsage)
			emitUsageOutputTokensDecision(ctx, s.sink, s.exchangeID, frameUsage)
			emitUsageCacheReadTokensDecision(ctx, s.sink, s.exchangeID, frameUsage)
			emitUsageCacheWriteTokensDecision(ctx, s.sink, s.exchangeID, frameUsage)
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
	s.enqueueEnvelopeStart(s.responseID, "", canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse})
	s.enqueue(canonical.Event{
		Kind:    canonical.EventMetadata,
		EnvID:   s.responseID,
		Payload: canonical.MetadataPayload{Values: map[string]string{"result_id": s.resultID, "model": s.model}},
	})
	return nil
}

func (s *messagesEventReader) handleContentBlockStart(raw string) error {
	var payload contentBlockStartFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return canonical.InternalError("messages stream content_block_start frame is invalid")
	}
	block := streamContentBlock{ItemID: "block_" + strconv.Itoa(payload.Index)}
	contentBlockType := strings.TrimSpace(payload.ContentBlock.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
	switch contentBlockType {
	case "text":
		block.ItemKind = canonical.ItemKindText
		block.ItemID = "text_" + strconv.Itoa(payload.Index)
		s.enqueueEnvelopeStart(s.blockEnvID(payload.Index), s.responseID, canonical.EnvelopeStartPayload{Kind: canonical.EnvMessage, Role: canonical.ItemAuthorAssistant}, canonical.EventMetadataFields{NativeID: block.ItemID})
	case "tool_use":
		block.ItemKind = canonical.ItemKindToolUse
		block.ToolUseID = strings.TrimSpace(payload.ContentBlock.ID) // swobu:io-string source=boundary
		if block.ToolUseID == "" {
			block.ToolUseID = "toolu_swobu_" + strconv.Itoa(payload.Index)
		}
		block.Name = strings.TrimSpace(payload.ContentBlock.Name) // swobu:io-string source=boundary
		block.ItemID = block.ToolUseID
		s.enqueueEnvelopeStart(s.blockEnvID(payload.Index), s.responseID, canonical.EnvelopeStartPayload{Kind: canonical.EnvToolCall, Name: block.Name, ToolUseID: block.ToolUseID}, canonical.EventMetadataFields{NativeID: block.ItemID})
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
	_, ok := s.blocks[payload.Index]
	if !ok {
		return nil
	}
	deltaType := strings.TrimSpace(payload.Delta.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
	switch deltaType {
	case "text_delta":
		s.enqueue(canonical.Event{
			Kind:    canonical.EventTextDelta,
			EnvID:   s.blockEnvID(payload.Index),
			Payload: canonical.TextDeltaPayload{Text: payload.Delta.Text},
		})
	case "input_json_delta":
		s.enqueue(canonical.Event{
			Kind:    canonical.EventArgsDelta,
			EnvID:   s.blockEnvID(payload.Index),
			Payload: canonical.ArgsDeltaPayload{Args: payload.Delta.PartialJSON},
		})
	default:
		return canonical.InternalError("messages stream delta type is unsupported")
	}
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
	s.enqueueEnvelopeEnd(s.blockEnvID(payload.Index), s.blockKind(block), canonical.EnvelopeStatusCompleted)
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
	deliverycompat.EmitTerminalEventDecision(ctx, s.sink, s.exchangeID, !s.latestUsage.IsZero())
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

func (s *messagesEventReader) blockKind(block streamContentBlock) canonical.EnvelopeKind {
	if block.ItemKind == canonical.ItemKindToolUse {
		return canonical.EnvToolCall
	}
	return canonical.EnvMessage
}
