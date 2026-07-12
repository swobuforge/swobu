// event-state machine together so migration behavior stays recoverable.
package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	deliverycompat "github.com/swobuforge/swobu/internal/adapters/wire/families/deliverycompat"
	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	openaicompat "github.com/swobuforge/swobu/internal/adapters/wire/shared/openaicompat"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
)

type responseEnvelope struct {
	ID         string                       `json:"id"`
	Model      string                       `json:"model"`
	OutputText string                       `json:"output_text"`
	Output     []responsesWireOutputItemDTO `json:"output"`
}

var tokenUsagePathSpec = core.TokenUsagePathSpec{
	InputPaths: [][]string{
		{"usage", "input_tokens"},
		{"usage", "prompt_tokens"},
		{"response", "usage", "input_tokens"},
		{"response", "usage", "prompt_tokens"},
		{"usageMetadata", "promptTokenCount"},
		{"usage", "inputTokens"},
		{"response", "usage", "inputTokens"},
	},
	OutputPaths: [][]string{
		{"usage", "output_tokens"},
		{"usage", "completion_tokens"},
		{"response", "usage", "output_tokens"},
		{"response", "usage", "completion_tokens"},
		{"usageMetadata", "candidatesTokenCount"},
		{"usage", "outputTokens"},
		{"response", "usage", "outputTokens"},
	},
	ReasoningPaths: [][]string{
		{"usage", "output_tokens_details", "reasoning_tokens"},
		{"response", "usage", "output_tokens_details", "reasoning_tokens"},
	},
	CacheReadPaths: [][]string{
		{"usage", "input_tokens_details", "cached_tokens"},
		{"usage", "prompt_tokens_details", "cached_tokens"},
		{"response", "usage", "input_tokens_details", "cached_tokens"},
		{"response", "usage", "prompt_tokens_details", "cached_tokens"},
		{"usage", "cache_read_input_tokens"},
		{"response", "usage", "cache_read_input_tokens"},
		{"usageMetadata", "cachedContentTokenCount"},
		{"usage", "cacheReadInputTokens"},
		{"response", "usage", "cacheReadInputTokens"},
	},
	CacheWritePaths: [][]string{
		{"usage", "input_tokens_details", "cache_write_tokens"},
		{"usage", "prompt_tokens_details", "cache_write_tokens"},
		{"response", "usage", "input_tokens_details", "cache_write_tokens"},
		{"response", "usage", "prompt_tokens_details", "cache_write_tokens"},
		{"usage", "cache_creation_input_tokens"},
		{"response", "usage", "cache_creation_input_tokens"},
		{"usage", "cacheWriteInputTokens"},
		{"response", "usage", "cacheWriteInputTokens"},
	},
}

func decodeResponseBuffered(ctx context.Context, raw []byte, exchangeID string, sink effect.Sink) (canonical.EventReader, error) {
	var dto responseEnvelope
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, canonical.InternalError("responses output is invalid JSON")
	}
	usage := core.ExtractTokenUsage(raw, tokenUsagePathSpec)
	emitUsageInputTokensDecision(ctx, sink, exchangeID, usage)
	emitUsageOutputTokensDecision(ctx, sink, exchangeID, usage)
	emitUsageReasoningTokensDecision(ctx, sink, exchangeID, usage)
	emitUsageCacheReadTokensDecision(ctx, sink, exchangeID, usage)
	emitUsageCacheWriteTokensDecision(ctx, sink, exchangeID, usage)
	items, err := decodeOutputItems(ctx, dto.Output, dto.OutputText, exchangeID, sink)
	if err != nil {
		return nil, err
	}
	return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		exchangeID,
		dto.ID,
		dto.Model,
		items,
		"completed",
		usage,
	)), nil
}

// DecodeResponseStream returns canonical envelope events directly for responses streams.
func decodeResponseStream(stream carrier.WireStream, exchangeID string, sink effect.Sink) canonical.EventReader {
	recording := &effect.RecordingSink{Delegate: sink}
	return &responsesEventReader{
		exchangeID:  exchangeID,
		responseID:  canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		sink:        recording,
		recording:   recording,
		reader:      core.NewSSEReader(carrier.ReadCloserFromFrameReader(stream.Frames)),
		toolStates:  map[string]responsesToolState{},
		toolInputs:  map[string]string{},
		textOpen:    false,
		latestUsage: canonical.NewUnknownTokenUsage(),
	}
}

type responsesEventReader struct {
	exchangeID  string
	responseID  canonical.EnvelopeID
	sink        effect.Sink
	recording   *effect.RecordingSink
	reader      *core.SSEReaderCloser
	pending     canonical.EventSequence
	toolStates  map[string]responsesToolState
	toolInputs  map[string]string
	textOpen    bool
	textEnvID   canonical.EnvelopeID
	started     bool
	completed   bool
	latestUsage canonical.TokenUsage
	seq         int64
}

func (s *responsesEventReader) Effects() []effect.Effect {
	if s.recording == nil {
		return nil
	}
	return append([]effect.Effect(nil), s.recording.Effects...)
}

type responsesToolState struct {
	envID    canonical.EnvelopeID
	toolType string
}

// ordered state machine over text, tool calls, and terminal frames.
// Reasoning is not part of the current canonical v0 grammar and must fail
// closed instead of disappearing from decode.
func (s *responsesEventReader) Next(ctx context.Context) (canonical.Event, error) {
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		return event, nil
	}
	for {
		event, err := s.reader.Next()
		if err != nil {
			if err == io.EOF && s.started && !s.completed {
				s.handleUnexpectedEOF(ctx)
				if len(s.pending) > 0 {
					out := s.pending[0]
					s.pending = s.pending[1:]
					return out, nil
				}
			}
			return canonical.Event{}, err
		}
		if strings.TrimSpace(event.Data) == "[DONE]" { // swobu:io-string source=boundary
			if s.started && !s.completed {
				s.handleStreamDone(ctx)
				if len(s.pending) > 0 {
					out := s.pending[0]
					s.pending = s.pending[1:]
					return out, nil
				}
			}
			continue
		}
		rawFrame := []byte(event.Data)
		frameUsage := core.ExtractTokenUsage(rawFrame, tokenUsagePathSpec)
		if !frameUsage.IsZero() {
			s.latestUsage = frameUsage
			emitUsageInputTokensDecision(ctx, s.sink, s.exchangeID, frameUsage)
			emitUsageOutputTokensDecision(ctx, s.sink, s.exchangeID, frameUsage)
			emitUsageReasoningTokensDecision(ctx, s.sink, s.exchangeID, frameUsage)
			emitUsageCacheReadTokensDecision(ctx, s.sink, s.exchangeID, frameUsage)
			emitUsageCacheWriteTokensDecision(ctx, s.sink, s.exchangeID, frameUsage)
		}
		var frame streamFrame
		if err := json.Unmarshal(rawFrame, &frame); err != nil {
			return canonical.Event{}, canonical.InternalError("responses stream event is invalid JSON")
		}
		handled, nextEvent, nextErr := s.handleFrame(ctx, frame)
		if nextErr != nil {
			return canonical.Event{}, nextErr
		}
		if handled {
			return nextEvent, nil
		}
	}
}

func (s *responsesEventReader) Close(context.Context) error {
	return s.reader.Close()
}

func (s *responsesEventReader) nextSeq() int64 {
	s.seq++
	return s.seq
}

func (s *responsesEventReader) enqueue(ev canonical.Event) {
	ev.ExchangeID = s.exchangeID
	ev.Seq = s.nextSeq()
	ev.Time = time.Now().UTC()
	s.pending = append(s.pending, ev)
}

func (s *responsesEventReader) enqueueEnvelopeStart(id canonical.EnvelopeID, parent canonical.EnvelopeID, payload canonical.EnvelopeStartPayload, meta ...canonical.EventMetadataFields) {
	ev := canonical.Event{Kind: canonical.EventEnvelopeStart, EnvID: id, ParentID: parent, Payload: payload}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	s.enqueue(ev)
}

func (s *responsesEventReader) enqueueEnvelopeEnd(id canonical.EnvelopeID, kind canonical.EnvelopeKind, status canonical.EnvelopeStatus) {
	s.enqueue(canonical.Event{Kind: canonical.EventEnvelopeEnd, EnvID: id, Payload: canonical.EnvelopeEndPayload{Kind: kind, Status: status}})
}

func (s *responsesEventReader) enqueueTextDelta(id canonical.EnvelopeID, text string) {
	s.enqueue(canonical.Event{Kind: canonical.EventTextDelta, EnvID: id, Payload: canonical.TextDeltaPayload{Text: text}})
}

func (s *responsesEventReader) enqueueArgsDelta(id canonical.EnvelopeID, args string) {
	s.enqueue(canonical.Event{Kind: canonical.EventArgsDelta, EnvID: id, Payload: canonical.ArgsDeltaPayload{Args: args}})
}

func (s *responsesEventReader) enqueueUsage(usage canonical.TokenUsage) {
	s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseID, Payload: canonical.UsagePayload{Usage: usage}})
}

func (s *responsesEventReader) enqueueFinish(reason string) {
	s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseID, Payload: canonical.FinishPayload{Reason: reason}})
}

func (s *responsesEventReader) enqueueMetadata(values map[string]string) {
	s.enqueue(canonical.Event{Kind: canonical.EventMetadata, EnvID: s.responseID, Payload: canonical.MetadataPayload{Values: values}})
}

func (s *responsesEventReader) enqueueError(code string, message string) {
	s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseID, Payload: canonical.ErrorPayload{Code: code, Message: message}})
}

func (s *responsesEventReader) handleUnexpectedEOF(ctx context.Context) {
	deliverycompat.EmitTerminalEventDecision(ctx, s.sink, s.exchangeID, false)
	s.enqueueError("stream_unexpected_eof", "output stream ended before completed")
	s.closeOpenText(canonical.EnvelopeStatusError)
	s.closeOpenTools(canonical.EnvelopeStatusError)
	s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusError)
	s.completed = true
}

func (s *responsesEventReader) handleStreamDone(ctx context.Context) {
	s.handleTerminalCompletion(ctx, "completed")
}

func (s *responsesEventReader) handleTerminalCompletion(ctx context.Context, status string) {
	normalizedStatus := strings.TrimSpace(status) // swobu:io-string source=provider-wire
	if normalizedStatus == "" {
		normalizedStatus = "completed"
	}
	s.completed = true
	deliverycompat.EmitTerminalEventDecision(ctx, s.sink, s.exchangeID, !s.latestUsage.IsZero())
	s.closeOpenText(canonical.EnvelopeStatusCompleted)
	s.closeOpenTools(canonical.EnvelopeStatusCompleted)
	s.enqueueUsage(s.latestUsage)
	s.enqueueFinish(normalizedStatus)
	s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
}

func (s *responsesEventReader) closeOpenText(status canonical.EnvelopeStatus) {
	if s.textOpen {
		s.enqueueEnvelopeEnd(s.textEnvID, canonical.EnvMessage, status)
		s.textOpen = false
		s.textEnvID = ""
	}
}

func (s *responsesEventReader) closeOpenTools(status canonical.EnvelopeStatus) {
	for itemID, state := range s.toolStates {
		s.enqueueEnvelopeEnd(state.envID, canonical.EnvToolCall, status)
		delete(s.toolStates, itemID)
		delete(s.toolInputs, itemID)
	}
}

func (s *responsesEventReader) ensureToolState(itemID string, toolType string, callID string, name string) responsesToolState {
	if state, ok := s.toolStates[itemID]; ok {
		if state.toolType == "" && strings.TrimSpace(toolType) != "" { // swobu:io-string source=domain
			state.toolType = strings.ToLower(strings.TrimSpace(toolType)) // swobu:io-string source=boundary
			s.toolStates[itemID] = state
		}
		return state
	}
	normalizedType := strings.ToLower(strings.TrimSpace(toolType)) // swobu:io-string source=boundary
	if normalizedType == "" {
		normalizedType = canonical.ToolTypeFunction
	}
	envID := canonical.EnvelopeID(fmt.Sprintf("%s:item:%s", s.responseID, itemID))
	state := responsesToolState{envID: envID, toolType: normalizedType}
	s.toolStates[itemID] = state
	s.enqueueEnvelopeStart(envID, s.responseID, canonical.EnvelopeStartPayload{
		Kind:      canonical.EnvToolCall,
		Name:      name,
		ToolUseID: callID,
		ToolType:  normalizedType,
	}, canonical.EventMetadataFields{NativeID: itemID})
	return state
}

func (s *responsesEventReader) enqueueToolArgs(itemID string, args string) {
	if args == "" { // swobu:io-string source=boundary
		return
	}
	state, ok := s.toolStates[itemID]
	if !ok {
		return
	}
	s.toolInputs[itemID] += args
	s.enqueueArgsDelta(state.envID, args)
}

func fallbackItemID(itemID string, callID string) string {
	if strings.TrimSpace(itemID) != "" { // swobu:io-string source=boundary
		return strings.TrimSpace(itemID) // swobu:io-string source=boundary
	}
	if strings.TrimSpace(callID) != "" { // swobu:io-string source=boundary
		return strings.TrimSpace(callID) // swobu:io-string source=boundary
	}
	return "tool_0"
}

func decodeOutputItems(ctx context.Context, items []responsesWireOutputItemDTO, outputText string, exchangeID string, sink effect.Sink) ([]canonical.OutputItem, error) {
	output := make([]canonical.OutputItem, 0, len(items))
	for idx, item := range items {
		itemType := strings.TrimSpace(item.Type) // swobu:io-string source=provider-wire
		switch itemType {
		case "message":
			parts, err := openaicompat.DecodeContentParts(item.Content, "responses message content is invalid")
			if err != nil {
				return nil, canonical.InternalError("responses message content is invalid")
			}
			err = openaicompat.WalkContentParts(parts, func(idx int, part openaicompat.ContentPartItem) error {
				partType := strings.TrimSpace(part.Type) // swobu:io-string source=boundary
				switch partType {
				case "text", "output_text", "input_text":
					output = append(output, canonical.NewTextOutputItem(fmt.Sprintf("text_%d", len(output)+idx), part.Text))
				default:
					return canonical.UnsupportedOperation("responses output item content part type is not implemented")
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		case "function_call", "mcp_call":
			rawArgs := item.Arguments
			if rawArgs != "" {
				decoded := map[string]any{}
				if err := json.Unmarshal([]byte(rawArgs), &decoded); err != nil {
					return nil, canonical.InternalError("responses tool call arguments are invalid")
				}
			}
			itemID := strings.TrimSpace(item.ID) // swobu:io-string source=boundary
			if itemID == "" {
				itemID = fallbackItemID("", item.CallID)
			}
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			name := strings.TrimSpace(item.Name)     // swobu:io-string source=boundary
			if callID == "" {
				callID = itemID
			}
			output = append(output, canonical.NewToolUseOutputItem(
				itemID,
				callID,
				name,
				canonical.NewToolArgumentsObject(rawArgs),
			))
		case "custom_tool_call":
			itemID := strings.TrimSpace(item.ID) // swobu:io-string source=boundary
			if itemID == "" {
				itemID = fallbackItemID("", item.CallID)
			}
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			name := strings.TrimSpace(item.Name)     // swobu:io-string source=boundary
			if callID == "" {
				callID = itemID
			}
			output = append(output, canonical.NewCustomToolUseOutputItem(
				itemID,
				callID,
				name,
				canonical.NewToolArgumentsObject(item.Input),
			))
		case "reasoning":
			if sink != nil {
				_ = sink.Commit(ctx, exchangeID, []effect.Effect{
					effect.Compatibility{
						Feature: compat.ResponseReasoning,
						Outcome: compat.Reject,
						Subject: compat.Subject(fmt.Sprintf("wire:/output/%d/type", idx)),
					},
				})
			}
			return nil, canonical.UnsupportedOperation("responses reasoning output is not supported by swobu v0")
		default:
			return nil, canonical.UnsupportedOperation("responses output item type is not implemented")
		}
	}
	if len(output) == 0 && strings.TrimSpace(outputText) != "" { // swobu:io-string source=boundary
		output = append(output, canonical.NewTextOutputItem("text_0", outputText))
	}
	return output, nil
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

func emitUsageReasoningTokensDecision(ctx context.Context, sink effect.Sink, exchangeID string, usage canonical.TokenUsage) {
	if sink == nil {
		return
	}
	if _, ok := usage.ReasoningTokens(); !ok {
		return
	}
	_ = sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.Compatibility{
			Feature: compat.UsageReasoningTokens,
			Outcome: compat.Exact,
			Subject: compat.Subject("wire:/usage/output_tokens_details/reasoning_tokens"),
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
