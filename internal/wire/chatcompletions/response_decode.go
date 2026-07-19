// Keep the event-state machine together so migration behavior stays recoverable.
package chatcompletions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	deliverycompat "github.com/swobuforge/swobu/internal/wire/deliverycompat"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

type responseBody struct {
	ID      string             `json:"id"`
	Model   string             `json:"model"`
	Choices []streamChoiceBody `json:"choices"`
}

type streamChoiceBody struct {
	Message struct {
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		ToolCalls []toolCallBody  `json:"tool_calls"`
	} `json:"message"`
	Delta struct {
		Role      string               `json:"role"`
		Content   string               `json:"content"`
		ToolCalls []streamToolCallBody `json:"tool_calls"`
	} `json:"delta"`
	FinishReason        string `json:"finish_reason"`
	ContentFilterResult struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	} `json:"content_filter_result"`
}

type streamToolCallBody struct {
	Index    int                    `json:"index"`
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function streamToolFunctionBody `json:"function"`
}

type streamToolFunctionBody struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

var tokenUsagePathSpec = core.TokenUsagePathSpec{
	InputPaths: [][]string{
		{"usage", "prompt_tokens"},
		{"usage", "input_tokens"},
		{"usageMetadata", "promptTokenCount"},
		{"usage", "inputTokens"},
	},
	OutputPaths: [][]string{
		{"usage", "completion_tokens"},
		{"usage", "output_tokens"},
		{"usageMetadata", "candidatesTokenCount"},
		{"usage", "outputTokens"},
	},
	ReasoningPaths: [][]string{
		{"usage", "completion_tokens_details", "reasoning_tokens"},
	},
	CacheReadPaths: [][]string{
		{"usage", "prompt_tokens_details", "cached_tokens"},
		{"usage", "input_tokens_details", "cached_tokens"},
		{"usage", "cache_read_input_tokens"},
		{"usageMetadata", "cachedContentTokenCount"},
		{"usage", "cacheReadInputTokens"},
	},
	CacheWritePaths: [][]string{
		{"usage", "prompt_tokens_details", "cache_write_tokens"},
		{"usage", "input_tokens_details", "cache_write_tokens"},
		{"usage", "cache_creation_input_tokens"},
		{"usage", "cacheWriteInputTokens"},
	},
}

func decodeResponseBuffered(ctx context.Context, raw []byte, exchangeID string, sink compat.Sink) (canonical.ResponseStream, error) {
	var dto responseBody
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, canonical.InternalError("chat completions response is invalid JSON")
	}
	if len(dto.Choices) == 0 {
		return nil, canonical.InternalError("chat completions response is missing choices")
	}
	choice := dto.Choices[0]
	usage := core.ExtractTokenUsage(raw, tokenUsagePathSpec)
	_, inputPresent := usage.InputTokens()
	openaiwire.EmitUsageDecision(ctx, sink, exchangeID, inputPresent, compat.ResponseUsageInputTokens, compat.Subject("wire:/usage/input_tokens"))
	_, outputPresent := usage.OutputTokens()
	openaiwire.EmitUsageDecision(ctx, sink, exchangeID, outputPresent, compat.ResponseUsageOutputTokens, compat.Subject("wire:/usage/output_tokens"))
	_, reasoningPresent := usage.ReasoningTokens()
	openaiwire.EmitUsageDecision(ctx, sink, exchangeID, reasoningPresent, compat.ResponseUsageReasoningTokens, compat.Subject("wire:/usage/completion_tokens_details/reasoning_tokens"))
	_, cacheReadPresent := usage.CacheReadTokens()
	openaiwire.EmitUsageDecision(ctx, sink, exchangeID, cacheReadPresent, compat.ResponseUsageCacheReadTokens, compat.Subject("wire:/usage/cache_read_tokens"))
	_, cacheWritePresent := usage.CacheWriteTokens()
	openaiwire.EmitUsageDecision(ctx, sink, exchangeID, cacheWritePresent, compat.ResponseUsageCacheWriteTokens, compat.Subject("wire:/usage/cache_write_tokens"))
	if openaiwire.IsContentFilterFinishReason(choice.FinishReason) {
		items, err := decodeResponseOutputItems(choice.Message.Content, choice.Message.ToolCalls)
		if err != nil {
			return nil, err
		}
		return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
			exchangeID,
			canonical.ResponseRef{},
			dto.Model,
			items,
			choice.FinishReason,
			usage,
		)), nil
	}
	items, err := decodeResponseOutputItems(choice.Message.Content, choice.Message.ToolCalls)
	if err != nil {
		return nil, err
	}
	return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		exchangeID,
		canonical.ResponseRef{},
		dto.Model,
		items,
		choice.FinishReason,
		usage,
	)), nil
}

// DecodeResponseStream returns canonical envelope events directly for chat completions streams.
func decodeResponseStream(stream carrier.ByteStream, exchangeID string, sink compat.Sink) *chatCompletionsEventReader {
	recording := &compat.RecordingSink{Delegate: sink}
	return &chatCompletionsEventReader{
		exchangeID:  exchangeID,
		responseID:  canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		sink:        recording,
		recording:   recording,
		reader:      core.NewSSEReader(stream.Body),
		toolCalls:   map[int]streamToolState{},
		toolEnvIDs:  map[int]canonical.EnvelopeID{},
		latestUsage: canonical.NewUnknownTokenUsage(),
	}
}

type chatCompletionsEventReader struct {
	exchangeID  string
	responseID  canonical.EnvelopeID
	sink        compat.Sink
	recording   *compat.RecordingSink
	reader      *core.SSEReaderCloser
	started     bool
	resultID    string
	model       string
	completed   bool
	pending     canonical.EventSequence
	textOpen    bool
	textEnvID   canonical.EnvelopeID
	toolCalls   map[int]streamToolState
	toolEnvIDs  map[int]canonical.EnvelopeID
	latestUsage canonical.TokenUsage
	seq         int64
}

func (s *chatCompletionsEventReader) Decisions() []compat.Decision {
	if s.recording == nil {
		return nil
	}
	return s.recording.Decisions()
}

type streamToolState struct {
	OutputItemID string
	ToolUseID    string
	Name         string
	PendingArgs  []string
}

// ordered state machine over text, tool calls, and terminal frames.
// variants while maintaining canonical output ordering.
func (s *chatCompletionsEventReader) Next(ctx context.Context) (canonical.Event, error) {
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		return event, nil
	}
	for {
		event, err := s.reader.Next(ctx)
		if err != nil {
			if err == io.EOF && s.started && !s.completed {
				deliverycompat.EmitTerminalUsagePresence(ctx, s.sink, s.exchangeID, false)
				s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseID, Payload: canonical.ErrorPayload{Code: "stream_unexpected_eof", Message: "output stream ended before completed"}})
				s.closeOpenChildren(canonical.EnvelopeStatusError)
				s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusError)
				s.completed = true
				if len(s.pending) > 0 {
					out := s.pending[0]
					s.pending = s.pending[1:]
					return out, nil
				}
			}
			return canonical.Event{}, err
		}
		if strings.TrimSpace(event.Data) == "[DONE]" { // swobu:io-string source=boundary
			continue
		}
		rawChunk := []byte(event.Data)
		chunkUsage := core.ExtractTokenUsage(rawChunk, tokenUsagePathSpec)
		if !chunkUsage.IsZero() {
			s.latestUsage = chunkUsage
			_, inputPresent := chunkUsage.InputTokens()
			openaiwire.EmitUsageDecision(ctx, s.sink, s.exchangeID, inputPresent, compat.ResponseUsageInputTokens, compat.Subject("wire:/usage/input_tokens"))
			_, outputPresent := chunkUsage.OutputTokens()
			openaiwire.EmitUsageDecision(ctx, s.sink, s.exchangeID, outputPresent, compat.ResponseUsageOutputTokens, compat.Subject("wire:/usage/output_tokens"))
			_, reasoningPresent := chunkUsage.ReasoningTokens()
			openaiwire.EmitUsageDecision(ctx, s.sink, s.exchangeID, reasoningPresent, compat.ResponseUsageReasoningTokens, compat.Subject("wire:/usage/completion_tokens_details/reasoning_tokens"))
			_, cacheReadPresent := chunkUsage.CacheReadTokens()
			openaiwire.EmitUsageDecision(ctx, s.sink, s.exchangeID, cacheReadPresent, compat.ResponseUsageCacheReadTokens, compat.Subject("wire:/usage/cache_read_tokens"))
			_, cacheWritePresent := chunkUsage.CacheWriteTokens()
			openaiwire.EmitUsageDecision(ctx, s.sink, s.exchangeID, cacheWritePresent, compat.ResponseUsageCacheWriteTokens, compat.Subject("wire:/usage/cache_write_tokens"))
		}
		var chunk responseBody
		if err := json.Unmarshal(rawChunk, &chunk); err != nil {
			return canonical.Event{}, canonical.InternalError("chat completions stream chunk is invalid JSON")
		}
		if !s.started {
			s.started = true
			s.resultID = chunk.ID
			s.model = chunk.Model
			s.enqueueEnvelopeStart(s.responseID, "", canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse})
			s.enqueue(canonical.Event{
				Kind:    canonical.EventMetadata,
				EnvID:   s.responseID,
				Payload: canonical.MetadataPayload{Values: map[string]string{"model": chunk.Model}},
			})
		}
		if len(chunk.Choices) == 0 {
			if len(s.pending) > 0 {
				return s.shiftPending(), nil
			}
			continue
		}
		choice := chunk.Choices[0]
		if openaiwire.IsContentFilterFinishReason(choice.FinishReason) {
			s.handleChoiceContentFilter(ctx, choice)
		} else {
			if err := s.applyChoiceDelta(choice); err != nil {
				return canonical.Event{}, err
			}
			s.applyChoiceFinish(ctx, choice)
		}
		if len(s.pending) > 0 {
			return s.shiftPending(), nil
		}
	}
}

func (s *chatCompletionsEventReader) applyChoiceDelta(choice streamChoiceBody) error {
	if choice.Delta.Content != "" {
		if !s.textOpen {
			s.textOpen = true
			s.textEnvID = canonical.EnvelopeID(fmt.Sprintf("%s:item:text_0", s.responseID))
			s.enqueueEnvelopeStart(s.textEnvID, s.responseID, canonical.EnvelopeStartPayload{Kind: canonical.EnvMessage, Role: canonical.ItemAuthorAssistant}, canonical.EventMetadataFields{NativeID: "text_0"})
		}
		s.enqueue(canonical.Event{Kind: canonical.EventTextDelta, EnvID: s.textEnvID, Payload: canonical.TextDeltaPayload{Text: choice.Delta.Content}})
	}
	for _, call := range choice.Delta.ToolCalls {
		if err := s.queueToolCallDelta(call); err != nil {
			return err
		}
	}
	return nil
}

func (s *chatCompletionsEventReader) applyChoiceFinish(ctx context.Context, choice streamChoiceBody) {
	if strings.TrimSpace(choice.FinishReason) == "" || s.completed { // swobu:io-string source=boundary
		return
	}
	if s.textOpen {
		s.enqueueEnvelopeEnd(s.textEnvID, canonical.EnvMessage, canonical.EnvelopeStatusCompleted)
		s.textOpen = false
	}
	for idx := range s.toolCalls {
		if envID := s.toolEnvIDs[idx]; envID != "" {
			s.enqueueEnvelopeEnd(envID, canonical.EnvToolCall, canonical.EnvelopeStatusCompleted)
		}
		delete(s.toolCalls, idx)
		delete(s.toolEnvIDs, idx)
	}
	s.completed = true
	deliverycompat.EmitTerminalUsagePresence(ctx, s.sink, s.exchangeID, !s.latestUsage.IsZero())
	s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseID, Payload: canonical.UsagePayload{Usage: s.latestUsage}})
	s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseID, Payload: canonical.FinishPayload{Reason: choice.FinishReason}})
	s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
}

func (s *chatCompletionsEventReader) handleChoiceContentFilter(ctx context.Context, choice streamChoiceBody) {
	if s.completed {
		return
	}
	s.completed = true
	deliverycompat.EmitTerminalUsagePresence(ctx, s.sink, s.exchangeID, !s.latestUsage.IsZero())
	s.closeOpenChildren(canonical.EnvelopeStatusError)
	s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseID, Payload: canonical.UsagePayload{Usage: s.latestUsage}})
	s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseID, Payload: canonical.FinishPayload{Reason: choice.FinishReason}})
	s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
}

func (s *chatCompletionsEventReader) Close(context.Context) error {
	return s.reader.Close()
}

func (s *chatCompletionsEventReader) shiftPending() canonical.Event {
	event := s.pending[0]
	s.pending = s.pending[1:]
	return event
}

func (s *chatCompletionsEventReader) queueToolCallDelta(call streamToolCallBody) error {
	state := s.toolCalls[call.Index]
	if state.OutputItemID == "" {
		state.OutputItemID = "tool_" + strconv.Itoa(call.Index)
	}
	if state.ToolUseID == "" {
		state.ToolUseID = strings.TrimSpace(call.ID) // swobu:io-string source=boundary
		if state.ToolUseID == "" {
			state.ToolUseID = "toolu_swobu_" + strconv.Itoa(call.Index)
		}
	}
	if strings.TrimSpace(call.Function.Name) != "" { // swobu:io-string source=boundary
		state.Name = strings.TrimSpace(call.Function.Name) // swobu:io-string source=boundary
	}
	if call.Function.Arguments != "" {
		state.PendingArgs = append(state.PendingArgs, call.Function.Arguments)
	}
	if s.toolEnvIDs[call.Index] == "" && strings.TrimSpace(state.Name) == "" { // swobu:io-string source=boundary
		s.toolCalls[call.Index] = state
		return nil
	}
	if s.toolEnvIDs[call.Index] == "" {
		envID := canonical.EnvelopeID(fmt.Sprintf("%s:item:%s", s.responseID, state.OutputItemID))
		s.toolEnvIDs[call.Index] = envID
		s.enqueueEnvelopeStart(envID, s.responseID, canonical.EnvelopeStartPayload{Kind: canonical.EnvToolCall, Name: state.Name, ToolUseID: state.ToolUseID}, canonical.EventMetadataFields{NativeID: state.OutputItemID})
	}
	for _, delta := range state.PendingArgs {
		s.enqueue(canonical.Event{Kind: canonical.EventArgsDelta, EnvID: s.toolEnvIDs[call.Index], Payload: canonical.ArgsDeltaPayload{Args: delta}})
	}
	state.PendingArgs = nil
	s.toolCalls[call.Index] = state
	return nil
}

func (s *chatCompletionsEventReader) nextSeq() int64 {
	s.seq++
	return s.seq
}

func (s *chatCompletionsEventReader) enqueue(ev canonical.Event) {
	ev.ExchangeID = s.exchangeID
	ev.Seq = s.nextSeq()
	ev.Time = time.Now().UTC()
	s.pending = append(s.pending, ev)
}

func (s *chatCompletionsEventReader) enqueueEnvelopeStart(id canonical.EnvelopeID, parent canonical.EnvelopeID, payload canonical.EnvelopeStartPayload, meta ...canonical.EventMetadataFields) {
	ev := canonical.Event{Kind: canonical.EventEnvelopeStart, EnvID: id, ParentID: parent, Payload: payload}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	s.enqueue(ev)
}

func (s *chatCompletionsEventReader) enqueueEnvelopeEnd(id canonical.EnvelopeID, kind canonical.EnvelopeKind, status canonical.EnvelopeStatus) {
	s.enqueue(canonical.Event{Kind: canonical.EventEnvelopeEnd, EnvID: id, Payload: canonical.EnvelopeEndPayload{Kind: kind, Status: status}})
}

func (s *chatCompletionsEventReader) enqueueError(code string, message string) {
	s.enqueue(canonical.Event{
		Kind:  canonical.EventError,
		EnvID: s.responseID,
		Payload: canonical.ErrorPayload{
			Code:    code,
			Message: message,
		},
	})
}

func (s *chatCompletionsEventReader) closeOpenChildren(status canonical.EnvelopeStatus) {
	if s.textOpen {
		s.enqueueEnvelopeEnd(s.textEnvID, canonical.EnvMessage, status)
		s.textOpen = false
	}
	for idx := range s.toolCalls {
		if envID := s.toolEnvIDs[idx]; envID != "" {
			s.enqueueEnvelopeEnd(envID, canonical.EnvToolCall, status)
		}
		delete(s.toolCalls, idx)
		delete(s.toolEnvIDs, idx)
	}
}
