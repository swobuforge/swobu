// swobu:lint ignore file-length because=chat-completions response decoding keeps ordered choice assembly in one protocol owner
// Keep the event-state machine together so migration behavior stays recoverable.
package chatcompletions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
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
	Index    int                     `json:"index"`
	ID       string                  `json:"id,omitempty"`
	Type     string                  `json:"type,omitempty"`
	Function *streamToolFunctionBody `json:"function"`
	Custom   *streamToolCustomBody   `json:"custom"`
}

type streamToolFunctionBody struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type streamToolCustomBody struct {
	Name  string `json:"name,omitempty"`
	Input string `json:"input,omitempty"`
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

func chatCompletion(reason string) canonical.Completion {
	switch strings.ToLower(strings.TrimSpace(reason)) { // swobu:io-string source=provider-wire
	case "length", "max_tokens", "max_output_tokens":
		return canonical.Incomplete(reason)
	case "content_filter", "content_filtered", "refusal", "safety", "guardrail_intervened":
		return canonical.Declined(reason)
	case "stop", "tool_calls", "function_call", "completed":
		return canonical.Completed(reason)
	default:
		return canonical.Failed(reason)
	}
}

func decodeResponseBuffered(ctx context.Context, request canonical.CanonicalRequest, names wire.ToolNames, raw []byte, exchangeID string, changeLog *[]compat.Change) (canonical.ResponseStream, error) {
	var dto responseBody
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, canonical.InternalError("chat completions response is invalid JSON")
	}
	if len(dto.Choices) == 0 {
		return nil, canonical.InternalError("chat completions response is missing choices")
	}
	choice := dto.Choices[0]
	usage := core.ExtractTokenUsage(raw, tokenUsagePathSpec)
	if openaiwire.IsContentFilterFinishReason(choice.FinishReason) {
		items, err := decodeChatChoiceItems(request, names, choice, changeLog, exchangeID)
		if err != nil {
			return nil, err
		}
		return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
			exchangeID,
			canonical.ResponseRef{},
			dto.Model,
			items,
			chatCompletion(choice.FinishReason),
			usage,
		)), nil
	}
	items, err := decodeChatChoiceItems(request, names, choice, changeLog, exchangeID)
	if err != nil {
		return nil, err
	}
	if err := validateChatResponseResidual(items, choice.FinishReason, choice.Message.Content, len(choice.Message.ToolCalls)); err != nil {
		return nil, err
	}
	return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		exchangeID,
		canonical.ResponseRef{},
		dto.Model,
		items,
		chatCompletion(choice.FinishReason),
		usage,
	)), nil
}

// DecodeResponseStream returns canonical envelope events directly for chat completions streams.
func decodeResponseStream(request canonical.CanonicalRequest, names wire.ToolNames, stream carrier.ByteStream, exchangeID string, _ *[]compat.Change) *chatCompletionsEventReader {
	reader := &chatCompletionsEventReader{
		exchangeID:      exchangeID,
		responseID:      canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		reader:          core.NewSSEReader(stream.Body),
		toolCalls:       map[int]streamToolState{},
		toolOccurrences: map[int]streamToolOccurrence{},
		latestUsage:     canonical.NewUnknownTokenUsage(),
		request:         request.Clone(),
		toolNames:       names,
	}
	reader.changeLog = &reader.changes
	return reader
}

type chatCompletionsEventReader struct {
	exchangeID      string
	responseID      canonical.EnvelopeID
	changeLog       *[]compat.Change
	changes         []compat.Change
	reader          *core.SSEReaderCloser
	started         bool
	resultID        string
	model           string
	completed       bool
	pending         canonical.EventSequence
	textOpen        bool
	textEnvID       canonical.EnvelopeID
	text            strings.Builder
	toolCalls       map[int]streamToolState
	toolOccurrences map[int]streamToolOccurrence
	sawToolCall     bool
	latestUsage     canonical.TokenUsage
	seq             int64
	request         canonical.CanonicalRequest
	toolNames       wire.ToolNames
}

func decodeChatChoiceItems(request canonical.CanonicalRequest, names wire.ToolNames, choice streamChoiceBody, changeLog *[]compat.Change, exchangeID string) ([]canonical.CanonicalItem, error) {
	items := make([]canonical.CanonicalItem, 0, 2+len(choice.Message.ToolCalls))
	output, err := decodeResponseOutputItems(request, names, choice.Message.Content, choice.Message.ToolCalls, changeLog, exchangeID)
	if err != nil {
		return nil, err
	}
	return append(items, output...), nil
}

func (s *chatCompletionsEventReader) Changes() []compat.Change {
	return compat.CloneChanges(s.changes)
}

type streamToolState struct {
	EnvID       canonical.EnvelopeID
	CallID      canonical.ToolCallID
	Tool        canonical.ToolKey
	WireCallID  string
	WireName    string
	Kind        string
	Args        string
	PendingArgs []string
	ArgDeltas   []string
	Started     bool
}

// streamToolOccurrence freezes the discriminator admitted for one provider
// tool-call index. The corresponding streamToolState freezes each non-empty
// call ID and tool name instead of treating complete identity as fragments.
// Erased and completed occurrences cannot be reclassified.
// Canonical order is assigned only when the terminal provider-index frontier
// is known; fragment arrival never owns semantic order.
type streamToolOccurrence struct {
	Kind      string
	Erased    bool
	Completed bool
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
		if err := s.admitResponseIdentity(chunk.ID, chunk.Model); err != nil {
			return canonical.Event{}, err
		}
		if !s.started {
			s.started = true
			s.enqueueEnvelopeStart(s.responseID, "", canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse, Model: chunk.Model})
			s.enqueue(canonical.Event{Kind: canonical.EventResponseIdentity, EnvID: s.responseID, Payload: canonical.ResponseIdentityPayload{Response: canonical.ResponseRef{}}})
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
			if err := s.applyChoiceFinish(ctx, choice); err != nil {
				return canonical.Event{}, err
			}
		}
		if len(s.pending) > 0 {
			return s.shiftPending(), nil
		}
	}
}

func (s *chatCompletionsEventReader) applyChoiceDelta(choice streamChoiceBody) error {
	if choice.Delta.Content != "" {
		if !s.textOpen {
			start, err := canonical.NewMessageStart(canonical.MessageRoleAssistant)
			if err != nil {
				return err
			}
			s.textOpen = true
			s.textEnvID = canonical.EnvelopeID(fmt.Sprintf("%s:item:text_0", s.responseID))
			s.enqueueItem(canonical.EventItemStart, s.textEnvID, s.textOrdinal(), start)
			s.enqueueItem(canonical.EventContentStart, s.textEnvID, s.textOrdinal(), canonical.NewMessageContentStart(canonical.PartKindText))
		}
		s.text.WriteString(choice.Delta.Content)
		s.enqueueItem(canonical.EventTextDelta, s.textEnvID, s.textOrdinal(), canonical.TextDeltaPayload{Text: choice.Delta.Content})
	}
	for _, call := range choice.Delta.ToolCalls {
		if call.Index < 0 {
			return canonical.NewBackendError("", 0, "chat completions streamed tool call index is negative", "")
		}
		kind, erased, err := admitChatToolCallUnion(call.Type, call.Function != nil, call.Custom != nil, false)
		if err != nil {
			return err
		}
		occurrence, admitted := s.toolOccurrences[call.Index]
		if !admitted {
			occurrence.Kind = kind
			occurrence.Erased = erased
			s.toolOccurrences[call.Index] = occurrence
			if !occurrence.Erased {
				if err := s.queueToolCallDelta(call); err != nil {
					return err
				}
				continue
			}
			if err := appendChatOccurrenceChange(s.changeLog, s.exchangeID, canonical.ResponseItemsKind, compat.Omission, canonical.ResponseItemOccurrence(uint32(call.Index))); err != nil {
				return err
			}
			delete(s.toolCalls, call.Index)
			continue
		}
		if occurrence.Completed {
			return canonical.NewBackendError("", 0, "chat completions streamed tool call index was reused after completion", "")
		}
		if kind != "" && occurrence.Kind != "" && kind != occurrence.Kind {
			return canonical.NewBackendError("", 0, "chat completions streamed tool call changed type after admission", "")
		}
		if occurrence.Erased {
			continue
		}
		if occurrence.Kind == "" && kind != "" {
			occurrence.Kind = kind
			occurrence.Erased = erased
			s.toolOccurrences[call.Index] = occurrence
			if occurrence.Erased {
				if err := appendChatOccurrenceChange(s.changeLog, s.exchangeID, canonical.ResponseItemsKind, compat.Omission, canonical.ResponseItemOccurrence(uint32(call.Index))); err != nil {
					return err
				}
				delete(s.toolCalls, call.Index)
				continue
			}
		}
		if err := s.queueToolCallDelta(call); err != nil {
			return err
		}
	}
	return nil
}

func (s *chatCompletionsEventReader) applyChoiceFinish(ctx context.Context, choice streamChoiceBody) error {
	if strings.TrimSpace(choice.FinishReason) == "" || s.completed { // swobu:io-string source=boundary
		return nil
	}
	if err := s.validateToolOccurrenceFrontier(); err != nil {
		return err
	}
	if finishRequiresToolCall(choice.FinishReason) && !s.sawToolCall && len(s.toolCalls) == 0 {
		return canonical.NewBackendError("", 0, "chat completions finish reason requires a surviving tool call", "")
	}
	if s.hasErasedToolOccurrence() && !s.textOpen && !s.sawToolCall && len(s.toolCalls) == 0 {
		return canonical.NewBackendError("", 0, "backend produced no usable canonical output", "")
	}
	textPresent := s.textOpen
	if textPresent {
		item, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart(s.text.String())})
		if err != nil {
			return canonical.InternalError("chat completions streamed message is invalid")
		}
		s.enqueueItem(canonical.EventItemCompleted, s.textEnvID, s.textOrdinal(), canonical.ItemCompletedPayload{Item: item})
		s.textOpen = false
	}
	indices := make([]int, 0, len(s.toolCalls))
	for idx := range s.toolCalls {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	for position, idx := range indices {
		state := s.toolCalls[idx]
		if !state.Started {
			return canonical.NewBackendError("", 0, "chat completions tool call ended before its identity was resolved", "")
		}
		var input canonical.ToolInput
		switch state.Kind {
		case canonical.ToolTypeFunction:
			object, err := canonical.ParseJSONObject([]byte(state.Args))
			if err != nil {
				logInvalidStreamedToolArguments(s.exchangeID, s.resultID, idx, state, err)
				return canonical.NewBackendError("", 0, "chat completions streamed tool arguments are invalid", "")
			}
			input = canonical.NewJSONObjectToolInput(object)
		case canonical.ToolTypeCustom:
			input = canonical.NewTextToolInput(state.Args)
		default:
			return canonical.NewBackendError("", 0, "chat completions streamed tool kind is invalid", "")
		}
		item, err := canonical.NewToolCallItem(state.CallID, state.Tool, input)
		if err != nil {
			return canonical.NewBackendError("", 0, "chat completions streamed tool call is invalid", "")
		}
		ordinal := uint32(position)
		if textPresent {
			ordinal++
		}
		// Tool lifecycle events commit only after finish reveals the structural
		// base and provider-index order. Emitting them on first fragment would
		// make transport timing observable as canonical item order.
		start, err := canonical.NewToolCallStart(state.CallID, state.Tool)
		if err != nil {
			return err
		}
		s.enqueueItem(canonical.EventItemStart, state.EnvID, ordinal, start)
		for _, delta := range state.ArgDeltas {
			s.enqueueItem(canonical.EventArgsDelta, state.EnvID, ordinal, canonical.ArgsDeltaPayload{Args: delta})
		}
		s.enqueueItem(canonical.EventItemCompleted, state.EnvID, ordinal, canonical.ItemCompletedPayload{Item: item})
		s.sawToolCall = true
		delete(s.toolCalls, idx)
		occurrence := s.toolOccurrences[idx]
		occurrence.Completed = true
		s.toolOccurrences[idx] = occurrence
	}
	for idx, occurrence := range s.toolOccurrences {
		occurrence.Completed = true
		s.toolOccurrences[idx] = occurrence
	}
	s.completed = true
	s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseID, Payload: canonical.UsagePayload{Usage: s.latestUsage}})
	s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseID, Payload: canonical.FinishPayload{Completion: chatCompletion(choice.FinishReason)}})
	s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
	return nil
}

func logInvalidStreamedToolArguments(exchangeID, responseID string, index int, state streamToolState, err error) {
	slog.Warn("chat completions streamed tool arguments are invalid",
		"component", "protocol.chat_completions",
		"event", "streamed_tool_arguments_invalid",
		"exchange_id", exchangeID,
		"provider_response_id", responseID,
		"tool_call_index", index,
		"tool_call_id", state.WireCallID,
		"tool_name", state.WireName,
		"argument_bytes", len(state.Args),
		"argument_fragments", len(state.ArgDeltas),
		"parse_error_kind", streamedToolArgumentErrorKind(state.Args, err),
	)
}

func streamedToolArgumentErrorKind(arguments string, err error) string {
	if strings.TrimSpace(arguments) == "" {
		return "empty"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "duplicate key"):
		return "duplicate_key"
	case strings.Contains(message, "top-level value is not an object"):
		return "non_object"
	case strings.Contains(message, "trailing"):
		return "trailing_data"
	case !json.Valid([]byte(arguments)):
		return "invalid_json"
	default:
		return "invalid_object"
	}
}

func (s *chatCompletionsEventReader) validateToolOccurrenceFrontier() error {
	if len(s.toolOccurrences) == 0 {
		return nil
	}
	maxIndex := 0
	for index, occurrence := range s.toolOccurrences {
		if index > maxIndex {
			maxIndex = index
		}
		if occurrence.Kind == "" && !occurrence.Erased {
			return canonical.NewBackendError("", 0, "chat completions streamed tool call ended without an inferable variant", "")
		}
	}
	for index := 0; index <= maxIndex; index++ {
		if _, observed := s.toolOccurrences[index]; !observed {
			return canonical.NewBackendError("", 0, "chat completions streamed tool call indexes contain an unobserved gap", "")
		}
	}
	return nil
}

func (s *chatCompletionsEventReader) hasErasedToolOccurrence() bool {
	for _, occurrence := range s.toolOccurrences {
		if occurrence.Erased {
			return true
		}
	}
	return false
}

func (s *chatCompletionsEventReader) handleChoiceContentFilter(ctx context.Context, choice streamChoiceBody) {
	if s.completed {
		return
	}
	s.completed = true
	s.closeOpenChildren(canonical.EnvelopeStatusError)
	s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseID, Payload: canonical.UsagePayload{Usage: s.latestUsage}})
	s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseID, Payload: canonical.FinishPayload{Completion: chatCompletion(choice.FinishReason)}})
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

func (s *chatCompletionsEventReader) admitResponseIdentity(resultID, model string) error {
	var err error
	s.resultID, err = admitChatIdentityField(s.resultID, resultID, "response id")
	if err != nil {
		return err
	}
	s.model, err = admitChatIdentityField(s.model, model, "model")
	return err
}

func admitChatIdentityField(admitted, observed, field string) (string, error) {
	observed = strings.TrimSpace(observed) // swobu:io-string source=boundary
	if observed == "" {
		return admitted, nil
	}
	if admitted == "" {
		return observed, nil
	}
	if admitted != observed {
		return admitted, canonical.NewBackendError("", 0, "chat completions streamed "+field+" changed after admission", "")
	}
	return admitted, nil
}

func (s *chatCompletionsEventReader) queueToolCallDelta(call streamToolCallBody) error {
	state := s.toolCalls[call.Index]
	kind := strings.ToLower(strings.TrimSpace(call.Type))
	if kind == "" {
		kind = s.toolOccurrences[call.Index].Kind
	}
	if state.Kind == "" {
		state.Kind = kind
	}
	if state.EnvID == "" {
		state.EnvID = canonical.EnvelopeID(fmt.Sprintf("%s:item:tool_%d", s.responseID, call.Index))
	}
	var err error
	state.WireCallID, err = admitChatIdentityField(state.WireCallID, call.ID, "tool call id")
	if err != nil {
		return err
	}
	switch state.Kind {
	case canonical.ToolTypeFunction:
		if call.Function != nil {
			state.WireName, err = admitChatIdentityField(state.WireName, call.Function.Name, "tool name")
			if err != nil {
				return err
			}
		}
		if call.Function != nil && call.Function.Arguments != "" {
			state.PendingArgs = append(state.PendingArgs, call.Function.Arguments)
			state.ArgDeltas = append(state.ArgDeltas, call.Function.Arguments)
		}
	case canonical.ToolTypeCustom:
		if call.Custom != nil {
			state.WireName, err = admitChatIdentityField(state.WireName, call.Custom.Name, "tool name")
			if err != nil {
				return err
			}
		}
		if call.Custom != nil && call.Custom.Input != "" {
			state.PendingArgs = append(state.PendingArgs, call.Custom.Input)
			state.ArgDeltas = append(state.ArgDeltas, call.Custom.Input)
		}
	}
	if !state.Started && (state.WireCallID == "" || state.WireName == "") {
		s.toolCalls[call.Index] = state
		return nil
	}
	if !state.Started {
		callID, err := canonical.NewToolCallID(state.WireCallID)
		if err != nil {
			return canonical.NewBackendError("", 0, "chat completions streamed tool call id is invalid", "")
		}
		environment, err := canonical.EffectiveTools(s.request)
		if err != nil {
			return canonical.InternalError("chat completions streamed tool environment is ambiguous")
		}
		key, err := wire.DecodeToolKey(s.toolNames, environment, canonical.ToolKind(state.Kind), state.WireName)
		if err != nil {
			return canonical.NewBackendError("", 0, "chat completions streamed tool name cannot be resolved against the effective request", "")
		}
		state.CallID = callID
		state.Tool = key
		state.Started = true
	}
	for _, delta := range state.PendingArgs {
		state.Args += delta
	}
	state.PendingArgs = nil
	s.toolCalls[call.Index] = state
	return nil
}

func validateChatResponseResidual(items []canonical.CanonicalItem, finishReason string, content json.RawMessage, wireToolCalls int) error {
	trimmedContent := strings.TrimSpace(string(content))
	if len(items) == 0 && (wireToolCalls > 0 || trimmedContent != "" && trimmedContent != "null" && trimmedContent != `""`) {
		return canonical.NewBackendError("", 0, "backend produced no usable canonical output", "")
	}
	if !finishRequiresToolCall(finishReason) {
		return nil
	}
	for _, item := range items {
		if _, ok := item.ToolCall(); ok {
			return nil
		}
	}
	return canonical.NewBackendError("", 0, "chat completions finish reason requires a surviving tool call", "")
}

func finishRequiresToolCall(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "tool_calls", "function_call":
		return true
	default:
		return false
	}
}
