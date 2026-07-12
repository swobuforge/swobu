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

	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
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
	FinishReason string `json:"finish_reason"`
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

func decodeResponseBuffered(raw []byte, exchangeID string) (canonical.EventReader, error) {
	var dto responseBody
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, canonical.InternalError("chat completions response is invalid JSON")
	}
	if len(dto.Choices) == 0 {
		return nil, canonical.InternalError("chat completions response is missing choices")
	}
	choice := dto.Choices[0]
	items, err := decodeResponseOutputItems(choice.Message.Content, choice.Message.ToolCalls)
	if err != nil {
		return nil, err
	}
	return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		exchangeID,
		dto.ID,
		dto.Model,
		items,
		choice.FinishReason,
		core.ExtractTokenUsage(raw, tokenUsagePathSpec),
	)), nil
}

// DecodeResponseStream returns canonical envelope events directly for chat completions streams.
func decodeResponseStream(stream carrier.WireStream, exchangeID string) canonical.EventReader {
	return &chatCompletionsEventReader{
		exchangeID:  exchangeID,
		responseID:  canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		reader:      core.NewSSEReader(carrier.ReadCloserFromFrameReader(stream.Frames)),
		toolCalls:   map[int]streamToolState{},
		toolEnvIDs:  map[int]canonical.EnvelopeID{},
		latestUsage: canonical.NewUnknownTokenUsage(),
	}
}

type chatCompletionsEventReader struct {
	exchangeID  string
	responseID  canonical.EnvelopeID
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

type streamToolState struct {
	OutputItemID string
	ToolUseID    string
	Name         string
	PendingArgs  []string
}

// ordered state machine over text, tool calls, and terminal frames.
// variants while maintaining canonical output ordering.
func (s *chatCompletionsEventReader) Next(context.Context) (canonical.Event, error) {
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		return event, nil
	}
	for {
		event, err := s.reader.Next()
		if err != nil {
			if err == io.EOF && s.started && !s.completed {
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
				Payload: canonical.MetadataPayload{Values: map[string]string{"result_id": chunk.ID, "model": chunk.Model}},
			})
		}
		if len(chunk.Choices) == 0 {
			if len(s.pending) > 0 {
				return s.shiftPending(), nil
			}
			continue
		}
		choice := chunk.Choices[0]
		if err := s.applyChoiceDelta(choice); err != nil {
			return canonical.Event{}, err
		}
		s.applyChoiceFinish(choice)
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

func (s *chatCompletionsEventReader) applyChoiceFinish(choice streamChoiceBody) {
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
