// event-state machine together so migration behavior stays recoverable.
package chatcompletions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type responseBody struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
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
	} `json:"choices"`
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

var tokenUsagePathSpec = protocols.TokenUsagePathSpec{
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

func DecodeResponseBuffered(raw []byte) (canonical.CanonicalOutputValue, error) {
	var dto responseBody
	if err := json.Unmarshal(raw, &dto); err != nil {
		return canonical.CanonicalOutputValue{}, canonical.InternalError("chat completions response is invalid JSON")
	}
	if len(dto.Choices) == 0 {
		return canonical.CanonicalOutputValue{}, canonical.InternalError("chat completions response is missing choices")
	}
	choice := dto.Choices[0]
	items, err := decodeResponseOutputItems(choice.Message.Content, choice.Message.ToolCalls)
	if err != nil {
		return canonical.CanonicalOutputValue{}, err
	}
	return canonical.NewConversationOutputWithUsage(
		dto.ID,
		dto.Model,
		items,
		choice.FinishReason,
		protocols.ExtractTokenUsage(raw, tokenUsagePathSpec),
	), nil
}

// DecodeResponseStream returns canonical envelope events directly for chat completions streams.
func DecodeResponseStream(body io.ReadCloser, exchangeID string) canonical.EventReader {
	return newStreamDecoder(body, exchangeID)
}

type canonicalOutputEventStreamCloser struct {
	exchangeID  string
	responseID  canonical.EnvelopeID
	reader      *protocols.SSEReaderCloser
	started     bool
	resultID    string
	model       string
	completed   bool
	pending     []canonical.Event
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
	Started      bool
	PendingArgs  []string
}

func newStreamDecoder(body io.ReadCloser, exchangeID string) *canonicalOutputEventStreamCloser {
	return &canonicalOutputEventStreamCloser{
		exchangeID:  exchangeID,
		responseID:  canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		reader:      protocols.NewSSEReader(body),
		toolCalls:   map[int]streamToolState{},
		toolEnvIDs:  map[int]canonical.EnvelopeID{},
		latestUsage: canonical.NewUnknownTokenUsage(),
	}
}

// ordered state machine over text, tool calls, and terminal frames.
// variants while maintaining canonical output ordering.
func (s *canonicalOutputEventStreamCloser) Next(context.Context) (canonical.Event, error) {
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
		if strings.TrimSpace(event.Data) == "[DONE]" { // trimlowerlint:allow boundary canonicalization
			continue
		}
		rawChunk := []byte(event.Data)
		chunkUsage := protocols.ExtractTokenUsage(rawChunk, tokenUsagePathSpec)
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
		if choice.Delta.Content != "" {
			if !s.textOpen {
				s.textOpen = true
				s.textEnvID = canonical.EnvelopeID(fmt.Sprintf("%s:item:text_0", s.responseID))
				s.enqueueEnvelopeStart(s.textEnvID, s.responseID, canonical.EnvelopeStartPayload{Kind: canonical.EnvMessage, Role: canonical.ItemAuthorAssistant}, canonical.EventMeta{NativeID: "text_0"})
			}
			s.enqueue(canonical.Event{Kind: canonical.EventTextDelta, EnvID: s.textEnvID, Payload: canonical.TextDeltaPayload{Text: choice.Delta.Content}})
		}
		for _, call := range choice.Delta.ToolCalls {
			if err := s.queueToolCallDelta(call); err != nil {
				return canonical.Event{}, err
			}
		}
		if strings.TrimSpace(choice.FinishReason) != "" && !s.completed { // trimlowerlint:allow boundary canonicalization
			if s.textOpen {
				s.enqueueEnvelopeEnd(s.textEnvID, canonical.EnvMessage, canonical.EnvelopeStatusCompleted)
				s.textOpen = false
			}
			for idx, state := range s.toolCalls {
				if state.Started {
					if envID := s.toolEnvIDs[idx]; envID != "" {
						s.enqueueEnvelopeEnd(envID, canonical.EnvToolCall, canonical.EnvelopeStatusCompleted)
					}
				}
				delete(s.toolCalls, idx)
				delete(s.toolEnvIDs, idx)
			}
			s.completed = true
			s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseID, Payload: canonical.UsagePayload{Usage: s.latestUsage}})
			s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseID, Payload: canonical.FinishPayload{Reason: choice.FinishReason}})
			s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
		}
		if len(s.pending) > 0 {
			return s.shiftPending(), nil
		}
	}
}

func (s *canonicalOutputEventStreamCloser) Close(context.Context) error {
	return s.reader.Close()
}

func (s *canonicalOutputEventStreamCloser) shiftPending() canonical.Event {
	event := s.pending[0]
	s.pending = s.pending[1:]
	return event
}

func (s *canonicalOutputEventStreamCloser) queueToolCallDelta(call streamToolCallBody) error {
	state := s.toolCalls[call.Index]
	if state.OutputItemID == "" {
		state.OutputItemID = "tool_" + strconv.Itoa(call.Index)
	}
	if state.ToolUseID == "" {
		state.ToolUseID = strings.TrimSpace(call.ID) // trimlowerlint:allow boundary canonicalization
		if state.ToolUseID == "" {
			state.ToolUseID = "toolu_swobu_" + strconv.Itoa(call.Index)
		}
	}
	if strings.TrimSpace(call.Function.Name) != "" { // trimlowerlint:allow boundary canonicalization
		state.Name = strings.TrimSpace(call.Function.Name) // trimlowerlint:allow boundary canonicalization
	}
	if call.Function.Arguments != "" {
		state.PendingArgs = append(state.PendingArgs, call.Function.Arguments)
	}
	if !state.Started && strings.TrimSpace(state.Name) == "" { // trimlowerlint:allow boundary canonicalization
		s.toolCalls[call.Index] = state
		return nil
	}
	if !state.Started {
		state.Started = true
		envID := canonical.EnvelopeID(fmt.Sprintf("%s:item:%s", s.responseID, state.OutputItemID))
		s.toolEnvIDs[call.Index] = envID
		s.enqueueEnvelopeStart(envID, s.responseID, canonical.EnvelopeStartPayload{Kind: canonical.EnvToolCall, Name: state.Name, ToolUseID: state.ToolUseID}, canonical.EventMeta{NativeID: state.OutputItemID})
	}
	for _, delta := range state.PendingArgs {
		s.enqueue(canonical.Event{Kind: canonical.EventArgsDelta, EnvID: s.toolEnvIDs[call.Index], Payload: canonical.ArgsDeltaPayload{Args: delta}})
	}
	state.PendingArgs = nil
	s.toolCalls[call.Index] = state
	return nil
}

func (s *canonicalOutputEventStreamCloser) nextSeq() int64 {
	s.seq++
	return s.seq
}

func (s *canonicalOutputEventStreamCloser) enqueue(ev canonical.Event) {
	ev.ExchangeID = s.exchangeID
	ev.Seq = s.nextSeq()
	ev.Time = time.Now().UTC()
	s.pending = append(s.pending, ev)
}

func (s *canonicalOutputEventStreamCloser) enqueueEnvelopeStart(id canonical.EnvelopeID, parent canonical.EnvelopeID, payload canonical.EnvelopeStartPayload, meta ...canonical.EventMeta) {
	ev := canonical.Event{Kind: canonical.EventEnvelopeStart, EnvID: id, ParentID: parent, Payload: payload}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	s.enqueue(ev)
}

func (s *canonicalOutputEventStreamCloser) enqueueEnvelopeEnd(id canonical.EnvelopeID, kind canonical.EnvelopeKind, status canonical.EnvelopeStatus) {
	s.enqueue(canonical.Event{Kind: canonical.EventEnvelopeEnd, EnvID: id, Payload: canonical.EnvelopeEndPayload{Kind: kind, Status: status}})
}

func (s *canonicalOutputEventStreamCloser) closeOpenChildren(status canonical.EnvelopeStatus) {
	if s.textOpen {
		s.enqueueEnvelopeEnd(s.textEnvID, canonical.EnvMessage, status)
		s.textOpen = false
	}
	for idx, state := range s.toolCalls {
		if state.Started {
			if envID := s.toolEnvIDs[idx]; envID != "" {
				s.enqueueEnvelopeEnd(envID, canonical.EnvToolCall, status)
			}
		}
		delete(s.toolCalls, idx)
		delete(s.toolEnvIDs, idx)
	}
}

func decodeResponseOutputItems(content json.RawMessage, toolCalls []toolCallBody) ([]canonical.OutputItem, error) {
	items, err := decodeOpenAIContentItems(content)
	if err != nil {
		return nil, canonical.InternalError("chat completions response content is unsupported")
	}
	out := make([]canonical.OutputItem, 0, len(items)+len(toolCalls))
	for idx, item := range items {
		if item.Kind != canonical.ItemKindText {
			continue
		}
		out = append(out, canonical.NewTextOutputItem("text_"+strconv.Itoa(idx), item.Text))
	}
	for _, call := range toolCalls {
		if call.Type != "" && call.Type != "function" {
			return nil, canonical.InternalError("chat completions response tool call type is unsupported")
		}
		input := map[string]any{}
		if strings.TrimSpace(call.Function.Arguments) != "" { // trimlowerlint:allow boundary canonicalization
			if err := json.Unmarshal([]byte(call.Function.Arguments), &input); err != nil {
				return nil, canonical.InternalError("chat completions response tool call arguments are invalid")
			}
		}
		itemID := strings.TrimSpace(call.ID) // trimlowerlint:allow boundary canonicalization
		if itemID == "" {
			itemID = "tool_0"
		}
		out = append(out, canonical.NewToolUseOutputItem(itemID, strings.TrimSpace(call.ID), strings.TrimSpace(call.Function.Name), input)) // trimlowerlint:allow boundary canonicalization
	}
	return out, nil
}

func decodeOpenAIContentItems(raw json.RawMessage) ([]canonical.CanonicalItem, error) {
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" { // trimlowerlint:allow boundary canonicalization
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text == "" {
			return nil, nil
		}
		return []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, text)}, nil
	}

	var parts []struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		InputText  string `json:"input_text"`
		OutputText string `json:"output_text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, err
	}
	decoded := make([]canonical.CanonicalItem, 0, len(parts))
	for _, part := range parts {
		partType := strings.TrimSpace(part.Type) // trimlowerlint:allow boundary canonicalization
		if partType == "" {
			partType = "text"
		}
		switch partType {
		case "text", "input_text", "output_text":
			text := part.Text
			if text == "" {
				text = part.InputText
			}
			if text == "" {
				text = part.OutputText
			}
			if text != "" {
				decoded = append(decoded, canonical.NewTextItem(canonical.ItemAuthorAssistant, text))
			}
		default:
			return nil, canonical.UnsupportedOperation("chat completions response content contains an unsupported part type")
		}
	}
	return decoded, nil
}
