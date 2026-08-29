package gemini

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

// interactionsStream reduces Gemini's closed text, thought-summary, and
// ordinary-function SSE grammar into canonical progressive events. Known
// steps outside that owned subset fail rather than becoming additive novelty.
type interactionsStream struct {
	request                canonical.CanonicalRequest
	exchangeID             string
	responseID             canonical.EnvelopeID
	interactionID          canonical.InteractionID
	reader                 *core.SSEReaderCloser
	pending                canonical.EventSequence
	steps                  map[int]interactionStep
	toolNames              wire.ToolNames
	changes                []compat.Change
	started                bool
	finished               bool
	seq                    int64
	nextItem               uint32
	nextStep               int
	completedFunctionCalls int
	completedSearchCalls   []canonical.ToolCallID
	usage                  canonical.TokenUsage
}

type interactionStep interface {
	itemOrdinal() uint32
	stepKind() string
}

type modelOutputStep struct {
	ordinal   uint32
	text      strings.Builder
	citations []canonical.WebCitation
}

func (step *modelOutputStep) itemOrdinal() uint32 { return step.ordinal }
func (*modelOutputStep) stepKind() string         { return "model_output" }

type thoughtStep struct {
	ordinal   uint32
	summary   strings.Builder
	signature string
}

func (step *thoughtStep) itemOrdinal() uint32 { return step.ordinal }
func (*thoughtStep) stepKind() string         { return "thought" }

type functionCallStep struct {
	ordinal        uint32
	payload        *interactionFunctionCall
	callID         canonical.ToolCallID
	tool           canonical.ToolKey
	started        bool
	argumentDeltas strings.Builder
}

func (step *functionCallStep) itemOrdinal() uint32 { return step.ordinal }
func (*functionCallStep) stepKind() string         { return "function_call" }

type interactionFunctionCall struct {
	ID        string
	Name      string
	Arguments canonical.JSONObject
}

type googleSearchCallStep struct {
	ordinal    uint32
	callID     canonical.ToolCallID
	searchType string
	arguments  json.RawMessage
	signature  string
}

func (step *googleSearchCallStep) itemOrdinal() uint32 { return step.ordinal }
func (*googleSearchCallStep) stepKind() string         { return "google_search_call" }

type googleSearchResultStep struct {
	ordinal   uint32
	callID    canonical.ToolCallID
	result    json.RawMessage
	isError   *bool
	signature string
}

func (step *googleSearchResultStep) itemOrdinal() uint32 { return step.ordinal }
func (*googleSearchResultStep) stepKind() string         { return "google_search_result" }

func newInteractionsStream(request canonical.CanonicalRequest, names wire.ToolNames, stream carrier.ByteStream, exchangeID string) *interactionsStream {
	return &interactionsStream{
		request:    request.Clone(),
		toolNames:  names,
		exchangeID: strings.TrimSpace(exchangeID),
		responseID: canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		reader:     core.NewSSEReader(stream.Body),
		steps:      make(map[int]interactionStep),
		usage:      canonical.NewUnknownTokenUsage(),
	}
}

func (s *interactionsStream) Changes() []compat.Change { return compat.CloneChanges(s.changes) }

func (s *interactionsStream) Next(ctx context.Context) (canonical.Event, error) {
	if len(s.pending) > 0 {
		return s.shift(), nil
	}
	for {
		frame, err := s.reader.Next(ctx)
		if err != nil {
			if err == io.EOF && s.started && !s.finished {
				s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseID, Payload: canonical.ErrorPayload{Code: "stream_unexpected_eof", Message: "Gemini Interactions stream ended before completion"}})
				s.enqueueEnvelopeEnd(canonical.EnvelopeStatusError)
				s.finished = true
				return s.shift(), nil
			}
			if err == io.EOF && !s.started {
				return canonical.Event{}, canonical.NewBackendError("gemini", 0, "Gemini Interactions stream ended before interaction.created", "")
			}
			return canonical.Event{}, err
		}
		if s.finished {
			return canonical.Event{}, canonical.NewBackendError("gemini", 0, "Gemini Interactions stream contained data after completion", "")
		}
		if strings.TrimSpace(frame.Data) == "" { // swobu:io-string source=provider-wire
			continue
		}
		var payload interactionSSEFrame
		if err := json.Unmarshal([]byte(frame.Data), &payload); err != nil {
			return canonical.Event{}, canonical.NewBackendError("gemini", 0, "Gemini Interactions stream frame is invalid JSON", "")
		}
		if err := s.handle(payload); err != nil {
			return canonical.Event{}, err
		}
		if len(s.pending) > 0 {
			return s.shift(), nil
		}
	}
}

func (s *interactionsStream) Close(context.Context) error { return s.reader.Close() }

type interactionSSEFrame struct {
	EventType   string `json:"event_type"`
	Interaction struct {
		ID     string                  `json:"id"`
		Model  string                  `json:"model"`
		Status string                  `json:"status"`
		Usage  interactionUsagePayload `json:"usage"`
	} `json:"interaction"`
	Index *int `json:"index"`
	Step  struct {
		Type       string          `json:"type"`
		ID         string          `json:"id"`
		Name       string          `json:"name"`
		Arguments  json.RawMessage `json:"arguments"`
		SearchType string          `json:"search_type"`
		CallID     string          `json:"call_id"`
		Result     []struct {
			URL     string `json:"url"`
			Title   string `json:"title"`
			Snippet string `json:"snippet"`
		} `json:"result"`
		Signature string `json:"signature"`
		Summary   []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"summary"`
	} `json:"step"`
	Delta struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Signature string          `json:"signature"`
		CallID    string          `json:"call_id"`
		Result    json.RawMessage `json:"result"`
		IsError   *bool           `json:"is_error"`
		Content   struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Annotation struct {
			Type       string  `json:"type"`
			URL        string  `json:"url"`
			Title      string  `json:"title"`
			Snippet    string  `json:"snippet"`
			StartIndex *uint32 `json:"start_index"`
			EndIndex   *uint32 `json:"end_index"`
		} `json:"annotation"`
		Annotations []struct {
			Type       string  `json:"type"`
			URL        string  `json:"url"`
			Title      string  `json:"title"`
			Snippet    string  `json:"snippet"`
			StartIndex *uint32 `json:"start_index"`
			EndIndex   *uint32 `json:"end_index"`
		} `json:"annotations"`
	} `json:"delta"`
	Usage     interactionUsagePayload `json:"usage"`
	StepUsage interactionUsagePayload `json:"step_usage"`
	Metadata  struct {
		TotalUsage interactionUsagePayload `json:"total_usage"`
	} `json:"metadata"`
	InteractionID string `json:"interaction_id"`
	Status        string `json:"status"`
	Error         struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type interactionUsagePayload struct {
	TotalInputTokens   *int `json:"total_input_tokens"`
	TotalOutputTokens  *int `json:"total_output_tokens"`
	TotalThoughtTokens *int `json:"total_thought_tokens"`
	TotalCachedTokens  *int `json:"total_cached_tokens"`
}

func (s *interactionsStream) handle(frame interactionSSEFrame) error {
	eventType := strings.TrimSpace(frame.EventType) // swobu:io-string source=provider-wire
	if eventType == "" {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions stream frame is missing event_type", "")
	}
	if !s.started && eventType != "interaction.created" && eventType != "error" {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions stream frame arrived before interaction.created", "")
	}
	switch eventType {
	case "interaction.created":
		return s.handleCreated(frame)
	case "step.start":
		return s.handleStepStart(frame)
	case "step.delta":
		return s.handleStepDelta(frame)
	case "step.stop":
		return s.handleStepStop(frame)
	case "interaction.completed":
		return s.handleCompleted(frame)
	case "interaction.status_update":
		return s.handleStatusUpdate(frame)
	case "error":
		return s.handleError(frame)
	default:
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions stream event_type is unsupported", "")
	}
}

func (s *interactionsStream) handleCreated(frame interactionSSEFrame) error {
	if s.started {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions stream received a second interaction.created", "")
	}
	interactionID := strings.TrimSpace(frame.Interaction.ID) // swobu:io-string source=provider-wire
	if interactionID == "" && s.request.PersistenceEligible() {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions created event is missing interaction id", "")
	}
	if strings.TrimSpace(frame.Interaction.Status) != "in_progress" { // swobu:io-string source=provider-wire
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions created event has an invalid status", "")
	}
	model := strings.TrimSpace(frame.Interaction.Model) // swobu:io-string source=provider-wire
	if model == "" {
		model = s.request.Model()
	}
	if strings.TrimSpace(model) == "" { // swobu:io-string source=boundary
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions created event is missing model", "")
	}
	s.enqueue(canonical.Event{Kind: canonical.EventEnvelopeStart, EnvID: s.responseID, Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse, Model: model}})
	s.interactionID = canonical.NewInteractionID(interactionID)
	response := canonical.ResponseRef{}
	if s.request.PersistenceEligible() {
		// Gemini first supplies its continuation handle at creation, while the
		// canonical checkpoint commits only after a completed response stream.
		// Keeping capture here preserves the provider lifecycle without making a
		// failed stream reusable state.
		response.Interactions = &canonical.InteractionsContinuation{ProviderInteractionID: s.interactionID}
	}
	s.enqueue(canonical.Event{Kind: canonical.EventResponseIdentity, EnvID: s.responseID, Payload: canonical.ResponseIdentityPayload{Response: response}})
	s.started = true
	return nil
}

func (s *interactionsStream) handleStepStart(frame interactionSSEFrame) error {
	index, err := requiredStepIndex(frame.Index)
	if err != nil {
		return err
	}
	if index != s.nextStep {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions step start index is non-contiguous", "")
	}
	s.nextStep++
	stepType := strings.TrimSpace(frame.Step.Type) // swobu:io-string source=provider-wire
	switch stepType {
	case "model_output":
		step := &modelOutputStep{ordinal: s.nextItem}
		s.nextItem++
		s.steps[index] = step
		start, err := canonical.NewMessageStart(canonical.MessageRoleAssistant)
		if err != nil {
			return err
		}
		s.enqueue(canonical.Event{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: step.ordinal}, Payload: start}})
		s.enqueue(canonical.Event{Kind: canonical.EventContentStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: step.ordinal}, Payload: canonical.NewMessageContentStart(canonical.PartKindText)}})
		return nil
	case "thought":
		// Summary is optional. Allocate a canonical item only after step.stop
		// proves that this thought contains readable reasoning.
		step := &thoughtStep{}
		for _, content := range frame.Step.Summary {
			if strings.TrimSpace(content.Type) != "text" {
				return canonical.NewBackendError("gemini", 0, "Gemini Interactions thought summary contains unsupported content", "")
			}
			step.summary.WriteString(content.Text)
		}
		s.steps[index] = step
		return nil
	case "function_call":
		step := &functionCallStep{ordinal: s.nextItem}
		s.nextItem++
		payload, err := geminiFunctionCallIdentityFromWire(frame.Step.ID, frame.Step.Name)
		if err != nil {
			return err
		}
		if len(frame.Step.Arguments) > 0 {
			payload.Arguments, err = geminiArgumentsFromWire(frame.Step.Arguments)
			if err != nil {
				return err
			}
		}
		if err := s.installFunctionCall(step, payload); err != nil {
			return err
		}
		s.steps[index] = step
		return nil
	case "google_search_call":
		step, err := newGoogleSearchCallStep(s.nextItem, frame)
		if err != nil {
			return err
		}
		s.nextItem++
		s.steps[index] = step
		start, err := canonical.NewToolCallStart(step.callID, canonical.WebSearchToolKey())
		if err != nil {
			return canonical.InternalError("Gemini Interactions Google Search call start is invalid")
		}
		s.enqueue(canonical.Event{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: step.ordinal}, Payload: start}})
		return nil
	case "google_search_result":
		step, err := s.newGoogleSearchResultStep(s.nextItem, frame)
		if err != nil {
			return err
		}
		s.nextItem++
		s.steps[index] = step
		return nil
	}
	if isKnownNonTextStep(stepType) {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions stream returned an unsupported step type", "")
	}
	// The published Step oneOf is closed. A new type is a provider contract
	// contradiction, not safely discardable additive output.
	return canonical.NewBackendError("gemini", 0, "Gemini Interactions stream received an unknown step type", "")
}

func (s *interactionsStream) handleStepDelta(frame interactionSSEFrame) error {
	index, err := requiredStepIndex(frame.Index)
	if err != nil {
		return err
	}
	step, exists := s.steps[index]
	if !exists {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions step delta arrived before start", "")
	}
	s.usage = mergeInteractionUsage(s.usage, frame.Metadata.TotalUsage)
	deltaType := strings.TrimSpace(frame.Delta.Type) // swobu:io-string source=provider-wire
	switch typed := step.(type) {
	case *modelOutputStep:
		if deltaType == "text" {
			typed.text.WriteString(frame.Delta.Text)
			s.enqueue(canonical.Event{Kind: canonical.EventTextDelta, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: typed.ordinal, Part: 0}, Payload: canonical.TextDeltaPayload{Text: frame.Delta.Text}}})
			return nil
		}
		if deltaType == "text_annotation_delta" {
			annotations := frame.Delta.Annotations
			if len(annotations) == 0 && strings.TrimSpace(frame.Delta.Annotation.Type) != "" {
				annotations = append(annotations, frame.Delta.Annotation)
			}
			if len(annotations) == 0 {
				return canonical.NewBackendError("gemini", 0, "Gemini Interactions text annotation delta is empty", "")
			}
			for _, annotation := range annotations {
				citation, err := geminiURLCitation(annotation.Type, annotation.URL, annotation.Title, annotation.Snippet, annotation.StartIndex, annotation.EndIndex, typed.text.String())
				if err != nil {
					return err
				}
				typed.citations = append(typed.citations, citation)
			}
			return nil
		}
	case *thoughtStep:
		if deltaType == "thought_summary" {
			if strings.TrimSpace(frame.Delta.Content.Type) != "text" {
				return canonical.NewBackendError("gemini", 0, "Gemini Interactions thought summary delta contains unsupported content", "")
			}
			typed.summary.WriteString(frame.Delta.Content.Text)
			return nil
		}
		if deltaType == "thought_signature" {
			signature := strings.TrimSpace(frame.Delta.Signature)
			if signature == "" {
				return canonical.NewBackendError("gemini", 0, "Gemini Interactions thought signature is empty", "")
			}
			if typed.signature != "" && typed.signature != signature {
				return canonical.NewBackendError("gemini", 0, "Gemini Interactions thought signature is contradictory", "")
			}
			typed.signature = signature
			return nil
		}
	case *functionCallStep:
		if deltaType == "function_call" {
			payload, err := geminiFunctionCallIdentityFromWire(frame.Delta.ID, frame.Delta.Name)
			if err != nil {
				return err
			}
			if len(frame.Delta.Arguments) > 0 {
				payload.Arguments, err = geminiArgumentsFromWire(frame.Delta.Arguments)
				if err != nil {
					return err
				}
			}
			if typed.payload == nil {
				return s.installFunctionCall(typed, payload)
			}
			if !geminiFunctionCallsEqual(*typed.payload, payload) {
				return canonical.NewBackendError("gemini", 0, "Gemini Interactions function call delta contradicts step start", "")
			}
			return nil
		}
		if deltaType == "arguments_delta" {
			return s.appendFunctionArguments(typed, frame.Delta.Arguments)
		}
	case *googleSearchCallStep:
		if deltaType == "google_search_call" {
			if len(frame.Delta.Arguments) > 0 {
				typed.arguments = append([]byte(nil), frame.Delta.Arguments...)
			}
			if err := mergeGeminiSearchSignature(&typed.signature, frame.Delta.Signature, "call"); err != nil {
				return err
			}
			return nil
		}
	case *googleSearchResultStep:
		if deltaType == "google_search_result" {
			typed.result = append([]byte(nil), frame.Delta.Result...)
			typed.isError = frame.Delta.IsError
			if err := mergeGeminiSearchSignature(&typed.signature, frame.Delta.Signature, "result"); err != nil {
				return err
			}
			return nil
		}
	}
	if isKnownNonTextDelta(deltaType) {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions stream returned an unsupported delta type", "")
	}
	return canonical.NewBackendError("gemini", 0, "Gemini Interactions text output received an unknown delta", "")
}

func (s *interactionsStream) handleStepStop(frame interactionSSEFrame) error {
	index, err := requiredStepIndex(frame.Index)
	if err != nil {
		return err
	}
	step, exists := s.steps[index]
	if !exists {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions step stop arrived before start", "")
	}
	s.usage = mergeInteractionUsage(s.usage, frame.Usage)
	delete(s.steps, index)
	switch typed := step.(type) {
	case *modelOutputStep:
		part, err := canonical.NewCitedTextMessagePart(typed.text.String(), typed.citations)
		if err != nil {
			return canonical.NewBackendError("gemini", 0, "Gemini Interactions URL citation is invalid", "")
		}
		message, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{part})
		if err != nil {
			return canonical.InternalError("Gemini Interactions text output is invalid")
		}
		s.enqueue(canonical.Event{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: typed.ordinal}, Payload: canonical.ItemCompletedPayload{Item: message}}})
		return nil
	case *thoughtStep:
		if typed.summary.Len() == 0 && typed.signature == "" {
			s.changes = compat.AppendUnique(s.changes, compat.NewOmission(canonical.ResponseItemsReasoning, canonical.ResponseItemOccurrence(s.nextItem)))
			return nil
		}
		typed.ordinal = s.nextItem
		s.nextItem++
		var parts []canonical.ReasoningPart
		if typed.summary.Len() > 0 {
			part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, typed.summary.String())
			if err != nil {
				return canonical.InternalError("Gemini Interactions thought summary is invalid")
			}
			parts = []canonical.ReasoningPart{part}
		}
		var opaque canonical.OpaqueThinking
		if typed.signature != "" {
			step := interactionInputStep{Type: "thought", Signature: typed.signature}
			if typed.summary.Len() > 0 {
				step.Summary = []interactionContent{{Type: "text", Text: typed.summary.String()}}
			}
			raw, err := json.Marshal(step)
			if err != nil {
				return canonical.InternalError("Gemini Interactions thought replay could not be encoded")
			}
			opaque, err = canonical.NewInteractionsOpaqueThinking(raw)
			if err != nil {
				return canonical.InternalError("Gemini Interactions thought replay is invalid")
			}
		}
		item, err := canonical.NewReasoningItem(parts, opaque)
		if err != nil {
			return canonical.InternalError("Gemini Interactions thought output is invalid")
		}
		s.enqueue(canonical.Event{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: typed.ordinal}, Payload: canonical.ItemCompletedPayload{Item: item}}})
		return nil
	case *functionCallStep:
		if typed.payload == nil || !typed.started {
			return canonical.NewBackendError("gemini", 0, "Gemini Interactions function call step is incomplete", "")
		}
		if typed.argumentDeltas.Len() > 0 {
			arguments, err := geminiArgumentsFromWire([]byte(typed.argumentDeltas.String()))
			if err != nil {
				return err
			}
			if !typed.payload.Arguments.IsEmpty() && typed.payload.Arguments.String() != arguments.String() {
				return canonical.NewBackendError("gemini", 0, "Gemini Interactions incremental function arguments contradict step start", "")
			}
			typed.payload.Arguments = arguments
		}
		if len(typed.payload.Arguments.Bytes()) == 0 {
			return canonical.NewBackendError("gemini", 0, "Gemini Interactions function call arguments are incomplete", "")
		}
		input := canonical.NewJSONObjectToolInput(typed.payload.Arguments)
		var item canonical.CanonicalItem
		var err error
		if typed.tool.Kind() == canonical.ToolKindDiscovery {
			declaration, available := canonical.EffectiveTools(s.request)
			if available != nil {
				return canonical.InternalError("Gemini Interactions function environment is ambiguous")
			}
			discoveryDeclaration, ok := declaration.Lookup(typed.tool)
			discovery, declared := discoveryDeclaration.Discovery()
			if !ok || !declared {
				return canonical.InternalError("Gemini Interactions discovery call lost its declaration")
			}
			item, err = canonical.NewToolDiscoveryCallItem(typed.callID, input, discovery.Executor())
		} else {
			item, err = canonical.NewToolCallItem(typed.callID, typed.tool, input)
		}
		if err != nil {
			return canonical.InternalError("Gemini Interactions function call is invalid")
		}
		s.enqueue(canonical.Event{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: typed.ordinal}, Payload: canonical.ItemCompletedPayload{Item: item}}})
		s.completedFunctionCalls++
		return nil
	case *googleSearchCallStep:
		search, _, err := typed.finish()
		if err != nil {
			return err
		}
		input, err := canonical.NewWebSearchToolInput(search)
		if err != nil {
			return canonical.InternalError("Gemini Interactions Google Search call is invalid")
		}
		item, err := canonical.NewToolCallItem(typed.callID, canonical.WebSearchToolKey(), input)
		if err != nil {
			return canonical.InternalError("Gemini Interactions Google Search call is invalid")
		}
		s.enqueue(canonical.Event{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: typed.ordinal}, Payload: canonical.ItemCompletedPayload{Item: item}}})
		s.completedSearchCalls = append(s.completedSearchCalls, typed.callID)
		return nil
	case *googleSearchResultStep:
		result, err := typed.finish()
		if err != nil {
			return err
		}
		item, err := canonical.NewWebSearchResultItem(typed.callID, result)
		if err != nil {
			return canonical.InternalError("Gemini Interactions Google Search result is invalid")
		}
		s.enqueue(canonical.Event{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: typed.ordinal}, Payload: canonical.ItemCompletedPayload{Item: item}}})
		return nil
	default:
		return canonical.InternalError("Gemini Interactions stream step state is invalid")
	}
}

func (s *interactionsStream) handleCompleted(frame interactionSSEFrame) error {
	if !s.started {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions completed event arrived before creation", "")
	}
	if len(s.steps) != 0 {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions completed with unfinished steps", "")
	}
	if interactionID := strings.TrimSpace(frame.Interaction.ID); !s.matchesInteractionID(interactionID) { // swobu:io-string source=provider-wire
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions completed event has an invalid interaction id", "")
	}
	s.usage = mergeInteractionUsage(s.usage, frame.Interaction.Usage)
	status := strings.TrimSpace(frame.Interaction.Status) // swobu:io-string source=provider-wire
	switch status {
	case "completed":
		s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseID, Payload: canonical.UsagePayload{Usage: s.usage}})
		s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseID, Payload: canonical.FinishPayload{Completion: canonical.Completed("completed")}})
		s.enqueueEnvelopeEnd(canonical.EnvelopeStatusCompleted)
	case "incomplete":
		s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseID, Payload: canonical.UsagePayload{Usage: s.usage}})
		s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseID, Payload: canonical.FinishPayload{Completion: canonical.Incomplete("incomplete")}})
		s.enqueueEnvelopeEnd(canonical.EnvelopeStatusCompleted)
	case "failed", "cancelled":
		s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseID, Payload: canonical.ErrorPayload{Code: status, Message: "Gemini interaction did not complete"}})
		s.enqueueEnvelopeEnd(canonical.EnvelopeStatusError)
	case "requires_action":
		if !s.hasCompletedFunctionCall() {
			return canonical.NewBackendError("gemini", 0, "Gemini Interactions requires_action completed without a function call", "")
		}
		s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseID, Payload: canonical.UsagePayload{Usage: s.usage}})
		s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseID, Payload: canonical.FinishPayload{Completion: canonical.Completed("requires_action")}})
		s.enqueueEnvelopeEnd(canonical.EnvelopeStatusCompleted)
	default:
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions completed with an unknown status", "")
	}
	s.finished = true
	return nil
}

func (s *interactionsStream) handleStatusUpdate(frame interactionSSEFrame) error {
	interactionID := strings.TrimSpace(frame.InteractionID) // swobu:io-string source=provider-wire
	if !s.matchesInteractionID(interactionID) {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions status update has an invalid interaction id", "")
	}
	status := strings.TrimSpace(frame.Status) // swobu:io-string source=provider-wire
	switch status {
	case "in_progress":
		return nil
	case "requires_action":
		return nil
	case "completed", "failed", "cancelled", "incomplete":
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions status update reached a terminal status before completion", "")
	default:
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions status update has an unknown status", "")
	}
}

func (s *interactionsStream) matchesInteractionID(raw string) bool {
	raw = strings.TrimSpace(raw)
	if s.interactionID == "" {
		return !s.request.PersistenceEligible() && raw == ""
	}
	return canonical.NewInteractionID(raw) == s.interactionID
}

func (s *interactionsStream) installFunctionCall(step *functionCallStep, payload interactionFunctionCall) error {
	if step == nil {
		return canonical.InternalError("Gemini Interactions function call state is invalid")
	}
	if step.payload != nil {
		if geminiFunctionCallsEqual(*step.payload, payload) {
			return nil
		}
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions function call is duplicated with different data", "")
	}
	environment, err := canonical.EffectiveTools(s.request)
	if err != nil {
		return canonical.InternalError("Gemini Interactions function environment is ambiguous")
	}
	// Gemini has one function-call grammar for every ordinary callable. Its
	// wire spelling does not distinguish canonical function and client-owned
	// discovery calls, so attempt provenance is the sole inverse authority.
	tool, err := wire.DecodeCallableKey(s.toolNames, environment, payload.Name)
	if err != nil {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions function call references an unknown tool name", "")
	}
	callID, err := canonical.NewToolCallID(payload.ID)
	if err != nil {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions function call is missing its id", "")
	}
	start, err := canonical.NewToolCallStart(callID, tool)
	if err != nil {
		return canonical.InternalError("Gemini Interactions function call start is invalid")
	}
	step.payload = &payload
	step.callID = callID
	step.tool = tool
	step.started = true
	s.enqueue(canonical.Event{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: step.ordinal}, Payload: start}})
	// Gemini may put an empty object on step.start as a placeholder before
	// sending the complete JSON object through arguments_delta. It carries no
	// argument bytes and must not prefix the later delta with "{}".
	if !payload.Arguments.IsEmpty() {
		s.enqueue(canonical.Event{Kind: canonical.EventArgsDelta, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: step.ordinal}, Payload: canonical.ArgsDeltaPayload{Args: payload.Arguments.String()}}})
	}
	return nil
}

func geminiFunctionCallFromWire(id, name string, rawArguments json.RawMessage) (interactionFunctionCall, error) {
	payload, err := geminiFunctionCallIdentityFromWire(id, name)
	if err != nil {
		return interactionFunctionCall{}, err
	}
	payload.Arguments, err = geminiArgumentsFromWire(rawArguments)
	return payload, err
}

func geminiFunctionCallIdentityFromWire(id, name string) (interactionFunctionCall, error) {
	id = strings.TrimSpace(id)     // swobu:io-string source=provider-wire
	name = strings.TrimSpace(name) // swobu:io-string source=provider-wire
	if id == "" || name == "" {
		return interactionFunctionCall{}, canonical.NewBackendError("gemini", 0, "Gemini Interactions function call is missing required data", "")
	}
	return interactionFunctionCall{ID: id, Name: name}, nil
}

func geminiArgumentsFromWire(rawArguments json.RawMessage) (canonical.JSONObject, error) {
	arguments, err := canonical.ParseJSONObject(rawArguments)
	if err != nil {
		return canonical.JSONObject{}, canonical.NewBackendError("gemini", 0, "Gemini Interactions function call arguments are not an object", "")
	}
	return arguments, nil
}

func geminiFunctionCallsEqual(left, right interactionFunctionCall) bool {
	return left.ID == right.ID && left.Name == right.Name && left.Arguments.String() == right.Arguments.String()
}

func (s *interactionsStream) appendFunctionArguments(step *functionCallStep, raw json.RawMessage) error {
	if step.payload == nil || len(raw) == 0 {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions arguments delta lacks function identity", "")
	}
	var fragment string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &fragment); err != nil {
			return canonical.NewBackendError("gemini", 0, "Gemini Interactions arguments delta is invalid", "")
		}
	} else {
		fragment = string(raw)
	}
	step.argumentDeltas.WriteString(fragment)
	s.enqueue(canonical.Event{Kind: canonical.EventArgsDelta, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: step.ordinal}, Payload: canonical.ArgsDeltaPayload{Args: fragment}}})
	return nil
}

func (step *googleSearchCallStep) finish() (canonical.WebSearchCall, []byte, error) {
	if strings.TrimSpace(step.signature) == "" {
		return canonical.WebSearchCall{}, nil, canonical.NewBackendError("gemini", 0, "Gemini Interactions Google Search call signature is missing", "")
	}
	if step.searchType != "" && strings.TrimSpace(step.searchType) != "web_search" {
		return canonical.WebSearchCall{}, nil, canonical.NewBackendError("gemini", 0, "Gemini Interactions Google Search subtype is unsupported", "")
	}
	var arguments struct {
		Queries []string `json:"queries"`
	}
	if len(step.arguments) == 0 || json.Unmarshal(step.arguments, &arguments) != nil {
		return canonical.WebSearchCall{}, nil, canonical.NewBackendError("gemini", 0, "Gemini Interactions Google Search arguments are invalid", "")
	}
	base := canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: arguments.Queries}
	raw, _ := json.Marshal(map[string]any{"type": "google_search_call", "id": step.callID.String(), "arguments": json.RawMessage(step.arguments), "search_type": "web_search", "signature": step.signature})
	refined, err := canonical.NewInteractionsWebSearchCall(base, raw)
	if err != nil {
		return canonical.WebSearchCall{}, nil, canonical.NewBackendError("gemini", 0, "Gemini Interactions Google Search arguments are invalid", "")
	}
	return refined, raw, nil
}

func (step *googleSearchResultStep) finish() (canonical.WebSearchResult, error) {
	if strings.TrimSpace(step.signature) == "" {
		return canonical.WebSearchResult{}, canonical.NewBackendError("gemini", 0, "Gemini Interactions Google Search result signature is missing", "")
	}
	if step.isError != nil && *step.isError {
		result, _ := canonical.NewWebSearchFailureResult("Gemini Google Search failed")
		raw, _ := json.Marshal(map[string]any{"type": "google_search_result", "call_id": step.callID.String(), "result": json.RawMessage(step.result), "is_error": true, "signature": step.signature})
		return result.WithInteractionsReplay(raw)
	}
	var rows []map[string]json.RawMessage
	if len(step.result) == 0 || json.Unmarshal(step.result, &rows) != nil || len(rows) == 0 {
		return canonical.WebSearchResult{}, canonical.NewBackendError("gemini", 0, "Gemini Interactions Google Search result shape is unsupported", "")
	}
	for _, row := range rows {
		if len(row) != 1 {
			return canonical.WebSearchResult{}, canonical.NewBackendError("gemini", 0, "Gemini Interactions Google Search result shape is unsupported", "")
		}
		rawSuggestion, ok := row["search_suggestions"]
		if !ok {
			return canonical.WebSearchResult{}, canonical.NewBackendError("gemini", 0, "Gemini Interactions Google Search result shape is unsupported", "")
		}
		var suggestion string
		if json.Unmarshal(rawSuggestion, &suggestion) != nil {
			return canonical.WebSearchResult{}, canonical.NewBackendError("gemini", 0, "Gemini Interactions Google Search result shape is unsupported", "")
		}
	}
	// Current Interactions result rows carry provider search suggestions, not
	// source URLs. Citation sources arrive later on model-output annotations and
	// remain owned by that message occurrence. The portable Search result is
	// therefore a successful zero-source occurrence plus exact native replay.
	result, _ := canonical.NewWebSearchResult(nil)
	raw, _ := json.Marshal(map[string]any{"type": "google_search_result", "call_id": step.callID.String(), "result": json.RawMessage(step.result), "is_error": false, "signature": step.signature})
	return result.WithInteractionsReplay(raw)
}

func mergeGeminiSearchSignature(current *string, incoming, occurrence string) error {
	incoming = strings.TrimSpace(incoming)
	if incoming == "" {
		return nil
	}
	if strings.TrimSpace(*current) != "" && *current != incoming {
		return canonical.NewBackendError("gemini", 0, "Gemini Interactions Google Search "+occurrence+" signature is contradictory", "")
	}
	*current = incoming
	return nil
}

func newGoogleSearchCallStep(ordinal uint32, frame interactionSSEFrame) (*googleSearchCallStep, error) {
	callID, err := canonical.NewToolCallID(strings.TrimSpace(frame.Step.ID))
	if err != nil {
		return nil, canonical.NewBackendError("gemini", 0, "Gemini Interactions Google Search call is missing its id", "")
	}
	if frame.Step.SearchType != "" && strings.TrimSpace(frame.Step.SearchType) != "web_search" {
		return nil, canonical.NewBackendError("gemini", 0, "Gemini Interactions Google Search subtype is unsupported", "")
	}
	return &googleSearchCallStep{ordinal: ordinal, callID: callID, searchType: frame.Step.SearchType, arguments: append([]byte(nil), frame.Step.Arguments...), signature: frame.Step.Signature}, nil
}

func (s *interactionsStream) newGoogleSearchResultStep(ordinal uint32, frame interactionSSEFrame) (*googleSearchResultStep, error) {
	callID, err := canonical.NewToolCallID(strings.TrimSpace(frame.Step.CallID))
	if err != nil {
		return nil, canonical.NewBackendError("gemini", 0, "Gemini Interactions Google Search result is missing call_id", "")
	}
	correlated := false
	for _, step := range s.steps {
		if call, ok := step.(*googleSearchCallStep); ok && call.callID == callID {
			correlated = true
		}
	}
	// Completed steps are removed, so also inspect the already emitted event
	// sequence through a compact response-local correlation ledger.
	if !correlated {
		for _, completed := range s.completedSearchCalls {
			if completed == callID {
				correlated = true
				break
			}
		}
	}
	if !correlated {
		return nil, canonical.NewBackendError("gemini", 0, "Gemini Interactions Google Search result has no prior call", "")
	}
	raw, _ := json.Marshal(frame.Step.Result)
	return &googleSearchResultStep{ordinal: ordinal, callID: callID, result: raw, signature: frame.Step.Signature}, nil
}

func geminiURLCitation(kind, rawURL, rawTitle, snippet string, start, end *uint32, text string) (canonical.WebCitation, error) {
	if strings.TrimSpace(kind) != "url_citation" {
		return canonical.WebCitation{}, canonical.NewBackendError("gemini", 0, "Gemini Interactions text annotation subtype is unsupported", "")
	}
	webURL, err := canonical.NewWebURL(strings.TrimSpace(rawURL))
	if err != nil {
		return canonical.WebCitation{}, canonical.NewBackendError("gemini", 0, "Gemini Interactions URL citation URL is invalid", "")
	}
	title := canonical.Unspecified[string]()
	if strings.TrimSpace(rawTitle) != "" {
		title = canonical.Specify(rawTitle)
	}
	source, err := canonical.NewWebSource(webURL, title)
	if err != nil {
		return canonical.WebCitation{}, canonical.NewBackendError("gemini", 0, "Gemini Interactions URL citation source is invalid", "")
	}
	citation := canonical.WebCitation{Source: source}
	if strings.TrimSpace(snippet) != "" {
		citation.Excerpt = canonical.Specify(snippet)
	}
	if start != nil {
		citation.Start = canonical.Specify(*start)
	}
	if end != nil {
		citation.End = canonical.Specify(*end)
	}
	if _, err := canonical.NewCitedTextMessagePart(text, []canonical.WebCitation{citation}); err != nil {
		return canonical.WebCitation{}, canonical.NewBackendError("gemini", 0, "Gemini Interactions URL citation offsets are invalid", "")
	}
	return citation, nil
}

func (s *interactionsStream) hasCompletedFunctionCall() bool {
	return s.completedFunctionCalls > 0
}

func (s *interactionsStream) handleError(frame interactionSSEFrame) error {
	if !s.started {
		return canonical.NewBackendError("gemini", 0, strings.TrimSpace(frame.Error.Message), "")
	}
	code := strings.TrimSpace(frame.Error.Code) // swobu:io-string source=provider-wire
	if code == "" {
		code = "gemini_error"
	}
	message := strings.TrimSpace(frame.Error.Message) // swobu:io-string source=provider-wire
	if message == "" {
		message = "Gemini interaction failed"
	}
	s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseID, Payload: canonical.ErrorPayload{Code: code, Message: message}})
	s.enqueueEnvelopeEnd(canonical.EnvelopeStatusError)
	s.finished = true
	return nil
}

func (s *interactionsStream) shift() canonical.Event {
	event := s.pending[0]
	s.pending = s.pending[1:]
	return event
}

func (s *interactionsStream) enqueue(event canonical.Event) {
	s.seq++
	event.ExchangeID = s.exchangeID
	event.Seq = s.seq
	event.Time = time.Now().UTC()
	s.pending = append(s.pending, event)
}

func (s *interactionsStream) enqueueEnvelopeEnd(status canonical.EnvelopeStatus) {
	s.enqueue(canonical.Event{Kind: canonical.EventEnvelopeEnd, EnvID: s.responseID, Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: status}})
}

func requiredStepIndex(index *int) (int, error) {
	if index == nil || *index < 0 {
		return 0, canonical.NewBackendError("gemini", 0, "Gemini Interactions stream step index is invalid", "")
	}
	return *index, nil
}

func isKnownNonTextStep(stepType string) bool {
	switch stepType {
	case "thought", "function_call", "function_result", "google_search_call", "google_search_result", "mcp_server_tool_call", "mcp_server_tool_result", "code_execution_call", "code_execution_result", "file_search_call", "file_search_result", "google_maps_call", "google_maps_result", "url_context_call", "url_context_result", "user_input":
		return true
	default:
		return false
	}
}

func isKnownNonTextDelta(deltaType string) bool {
	switch deltaType {
	case "arguments_delta", "thought_summary", "thought_signature", "text_annotation_delta", "function_result", "google_search_call", "google_search_result", "code_execution_call", "code_execution_result", "file_search_call", "file_search_result", "google_maps_call", "google_maps_result", "url_context_call", "url_context_result", "audio", "document", "image":
		return true
	default:
		return false
	}
}

func mergeInteractionUsage(previous canonical.TokenUsage, payload interactionUsagePayload) canonical.TokenUsage {
	input, _ := previous.InputTokens()
	output, _ := previous.OutputTokens()
	reasoning, _ := previous.ReasoningTokens()
	cacheRead, _ := previous.CacheReadTokens()
	inputKnown := false
	outputKnown := false
	reasoningKnown := false
	cacheReadKnown := false
	if value, known := previous.InputTokens(); known {
		inputKnown = true
		input = value
	}
	if value, known := previous.OutputTokens(); known {
		outputKnown = true
		output = value
	}
	if value, known := previous.ReasoningTokens(); known {
		reasoningKnown = true
		reasoning = value
	}
	if value, known := previous.CacheReadTokens(); known {
		cacheReadKnown = true
		cacheRead = value
	}
	if payload.TotalInputTokens != nil {
		input, inputKnown = *payload.TotalInputTokens, true
	}
	if payload.TotalOutputTokens != nil {
		output, outputKnown = *payload.TotalOutputTokens, true
	}
	if payload.TotalThoughtTokens != nil {
		reasoning, reasoningKnown = *payload.TotalThoughtTokens, true
	}
	if payload.TotalCachedTokens != nil {
		cacheRead, cacheReadKnown = *payload.TotalCachedTokens, true
	}
	pointer := func(value int, known bool) *int {
		if !known {
			return nil
		}
		return &value
	}
	merged, err := canonical.NewTokenUsage(canonical.TokenUsageParams{
		InputTokens: pointer(input, inputKnown), OutputTokens: pointer(output, outputKnown),
		ReasoningTokens: pointer(reasoning, reasoningKnown), CacheReadTokens: pointer(cacheRead, cacheReadKnown),
	})
	if err != nil {
		return previous
	}
	return merged
}

var _ canonical.ResponseStream = (*interactionsStream)(nil)
