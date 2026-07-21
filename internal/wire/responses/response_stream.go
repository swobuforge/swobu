package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/responsesnative"
	deliverycompat "github.com/swobuforge/swobu/internal/wire/deliverycompat"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

// DecodeResponseStream returns canonical envelope events directly for responses streams.
func decodeResponseStream(request canonical.CanonicalRequest, stream carrier.ByteStream, exchangeID string, sink compat.Sink) *responsesResponseStream {
	recording := &compat.RecordingSink{Delegate: sink}
	return &responsesResponseStream{
		exchangeID:      exchangeID,
		responseEnvID:   canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		sink:            recording,
		recording:       recording,
		reader:          core.NewSSEReader(stream.Body),
		toolStates:      map[string]responsesToolState{},
		toolInputs:      map[string]string{},
		reasoningStates: map[string]*responsesReasoningState{},
		latestUsage:     canonical.NewUnknownTokenUsage(),
		nativeItems:     map[int]json.RawMessage{},
		request:         request.Clone(),
	}
}

type responsesResponseStream struct {
	exchangeID      string
	responseEnvID   canonical.EnvelopeID
	sink            compat.Sink
	recording       *compat.RecordingSink
	reader          *core.SSEReaderCloser
	pending         canonical.EventSequence
	toolStates      map[string]responsesToolState
	toolInputs      map[string]string
	reasoningStates map[string]*responsesReasoningState
	textState       *responsesTextState
	emittedOutput   bool
	started         bool
	completed       bool
	latestUsage     canonical.TokenUsage
	seq             int64
	request         canonical.CanonicalRequest
	nextOrdinal     uint32
	// ordinalOffset is the signed cardinality delta between provider output
	// items and emitted canonical items. Expanded search results increase it;
	// intentionally omitted empty reasoning artifacts decrease it.
	ordinalOffset int64
	nativeMu      sync.RWMutex
	nativeItems   map[int]json.RawMessage
	nativeBatch   responsesnative.Items
}

type responsesReasoningStreamPartState struct {
	kind canonical.ReasoningPartKind
	text strings.Builder
}

type responsesReasoningState struct {
	id     string
	status string
	parts  []*responsesReasoningStreamPartState
}

// responsesTextState binds one open provider message to its canonical
// placement. Keeping both coordinate systems in one optional state prevents a
// partially open lifecycle after one-to-many item projection.
type responsesTextState struct {
	envID                  canonical.EnvelopeID
	ordinal                uint32
	providerOutputIndex    int
	hasProviderOutputIndex bool
	text                   strings.Builder
}

func newResponsesTextState(envID canonical.EnvelopeID, ordinal uint32, outputIndex *int) *responsesTextState {
	state := &responsesTextState{envID: envID, ordinal: ordinal}
	if outputIndex != nil {
		state.providerOutputIndex = *outputIndex
		state.hasProviderOutputIndex = true
	}
	return state
}

func (s *responsesTextState) accepts(outputIndex *int) bool {
	if outputIndex == nil {
		return true
	}
	return s != nil && s.hasProviderOutputIndex && s.providerOutputIndex == *outputIndex
}

func (s *responsesResponseStream) Decisions() []compat.Decision {
	if s.recording == nil {
		return nil
	}
	return s.recording.Decisions()
}

type responsesToolState struct {
	envID         canonical.EnvelopeID
	ordinal       uint32
	toolType      string
	callID        canonical.ToolCallID
	tool          canonical.ToolKey
	argumentsDone bool
	outputDone    bool
	closed        bool
}

// ordered state machine over text, tool calls, reasoning projection, and
// terminal frames. Exact native items are captured separately from these
// progressive portable events.
func (s *responsesResponseStream) Next(ctx context.Context) (canonical.Event, error) {
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		return event, nil
	}
	for {
		event, err := s.reader.Next(ctx)
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
			_, inputPresent := frameUsage.InputTokens()
			openaiwire.EmitUsageDecision(ctx, s.sink, s.exchangeID, inputPresent, compat.ResponseUsageInputTokens, compat.Subject("wire:/usage/input_tokens"))
			_, outputPresent := frameUsage.OutputTokens()
			openaiwire.EmitUsageDecision(ctx, s.sink, s.exchangeID, outputPresent, compat.ResponseUsageOutputTokens, compat.Subject("wire:/usage/output_tokens"))
			_, reasoningPresent := frameUsage.ReasoningTokens()
			openaiwire.EmitUsageDecision(ctx, s.sink, s.exchangeID, reasoningPresent, compat.ResponseUsageReasoningTokens, compat.Subject("wire:/usage/output_tokens_details/reasoning_tokens"))
			_, cacheReadPresent := frameUsage.CacheReadTokens()
			openaiwire.EmitUsageDecision(ctx, s.sink, s.exchangeID, cacheReadPresent, compat.ResponseUsageCacheReadTokens, compat.Subject("wire:/usage/cache_read_tokens"))
			_, cacheWritePresent := frameUsage.CacheWriteTokens()
			openaiwire.EmitUsageDecision(ctx, s.sink, s.exchangeID, cacheWritePresent, compat.ResponseUsageCacheWriteTokens, compat.Subject("wire:/usage/cache_write_tokens"))
		}
		var frame streamFrame
		if err := json.Unmarshal(rawFrame, &frame); err != nil {
			return canonical.Event{}, canonical.InternalError("responses stream event is invalid JSON")
		}
		var native struct {
			Item     json.RawMessage `json:"item"`
			Response struct {
				Output []json.RawMessage `json:"output"`
			} `json:"response"`
		}
		if err := json.Unmarshal(rawFrame, &native); err != nil {
			return canonical.Event{}, canonical.InternalError("responses stream event native item is invalid JSON")
		}
		frame.RawItem = native.Item
		frame.RawOutput = native.Response.Output
		if strings.TrimSpace(frame.Type) == "response.output_item.done" && frame.OutputIndex != nil && len(bytes.TrimSpace(frame.RawItem)) > 0 { // swobu:io-string source=provider-wire
			s.nativeItems[*frame.OutputIndex] = append(json.RawMessage(nil), frame.RawItem...)
		}
		handled, _, nextErr := s.handleFrame(ctx, frame)
		if nextErr != nil {
			return canonical.Event{}, nextErr
		}
		if handled {
			if len(s.pending) > 0 {
				return s.shiftPendingEvent(), nil
			}
			continue
		}
	}
}

func (s *responsesResponseStream) completeNativeOutput(terminal []json.RawMessage) error {
	items := terminal
	if len(items) == 0 && len(s.nativeItems) > 0 {
		items = make([]json.RawMessage, len(s.nativeItems))
		for index := range items {
			raw, ok := s.nativeItems[index]
			if !ok {
				return canonical.InternalError("responses stream native output order is incomplete")
			}
			items[index] = raw
		}
	}
	rawItems := make([][]byte, len(items))
	for index := range items {
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(items[index], &header); err != nil || strings.TrimSpace(header.Type) == "" { // swobu:io-string source=provider-wire
			return canonical.InternalError("responses stream output contains a malformed native item")
		}
		rawItems[index] = items[index]
	}
	batch, err := responsesnative.NewItems(rawItems)
	if err != nil {
		return canonical.InternalError("responses stream native output contains an invalid item")
	}
	s.nativeMu.Lock()
	s.nativeBatch = batch
	s.nativeMu.Unlock()
	return nil
}

func (s *responsesResponseStream) ResponsesOutput() (responsesnative.Items, bool) {
	s.nativeMu.RLock()
	defer s.nativeMu.RUnlock()
	return s.nativeBatch.Clone(), s.completed && !s.nativeBatch.IsZero()
}

func (s *responsesResponseStream) Close(context.Context) error {
	return s.reader.Close()
}

func (s *responsesResponseStream) nextSeq() int64 {
	s.seq++
	return s.seq
}

func (s *responsesResponseStream) enqueue(ev canonical.Event) {
	ev.ExchangeID = s.exchangeID
	ev.Seq = s.nextSeq()
	ev.Time = time.Now().UTC()
	s.pending = append(s.pending, ev)
}

func (s *responsesResponseStream) enqueueEnvelopeStart(id canonical.EnvelopeID, parent canonical.EnvelopeID, payload canonical.EnvelopeStartPayload, meta ...canonical.EventMetadataFields) {
	ev := canonical.Event{Kind: canonical.EventEnvelopeStart, EnvID: id, ParentID: parent, Payload: payload}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	s.enqueue(ev)
}

func (s *responsesResponseStream) enqueueEnvelopeEnd(id canonical.EnvelopeID, kind canonical.EnvelopeKind, status canonical.EnvelopeStatus) {
	s.enqueue(canonical.Event{Kind: canonical.EventEnvelopeEnd, EnvID: id, Payload: canonical.EnvelopeEndPayload{Kind: kind, Status: status}})
}

func (s *responsesResponseStream) enqueueTextDelta(id canonical.EnvelopeID, ordinal uint32, text string) {
	s.enqueue(canonical.Event{Kind: canonical.EventTextDelta, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: ordinal}, Payload: canonical.TextDeltaPayload{Text: text}}})
}

func (s *responsesResponseStream) enqueueArgsDelta(id canonical.EnvelopeID, ordinal uint32, args string) {
	s.enqueue(canonical.Event{Kind: canonical.EventArgsDelta, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: ordinal}, Payload: canonical.ArgsDeltaPayload{Args: args}}})
}

func (s *responsesResponseStream) enqueueItemStart(id canonical.EnvelopeID, ordinal uint32, start canonical.ItemStartPayload) {
	s.enqueue(canonical.Event{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: ordinal}, Payload: start}})
}

func (s *responsesResponseStream) enqueueItemCompleted(id canonical.EnvelopeID, ordinal uint32, item canonical.CanonicalItem) {
	s.enqueue(canonical.Event{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: ordinal}, Payload: canonical.ItemCompletedPayload{Item: item}}})
}

func (s *responsesResponseStream) enqueueUsage(usage canonical.TokenUsage) {
	s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseEnvID, Payload: canonical.UsagePayload{Usage: usage}})
}

func (s *responsesResponseStream) enqueueFinish(reason string) {
	s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseEnvID, Payload: canonical.FinishPayload{Reason: reason}})
}

func (s *responsesResponseStream) enqueueError(code string, message string) {
	s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseEnvID, Payload: canonical.ErrorPayload{Code: code, Message: message}})
}

func (s *responsesResponseStream) handleUnexpectedEOF(ctx context.Context) {
	deliverycompat.EmitTerminalUsagePresence(ctx, s.sink, s.exchangeID, false)
	s.enqueueError("stream_unexpected_eof", "output stream ended before completed")
	s.closeOpenText(canonical.EnvelopeStatusError)
	s.closeOpenTools(canonical.EnvelopeStatusError)
	s.enqueueEnvelopeEnd(s.responseEnvID, canonical.EnvResponse, canonical.EnvelopeStatusError)
	s.completed = true
}

func (s *responsesResponseStream) handleStreamDone(ctx context.Context) {
	s.handleTerminalCompletion(ctx, "completed")
}

func (s *responsesResponseStream) handleTerminalCompletion(ctx context.Context, status string) {
	normalizedStatus := strings.TrimSpace(status) // swobu:io-string source=provider-wire
	if normalizedStatus == "" {
		normalizedStatus = "completed"
	}
	s.completed = true
	deliverycompat.EmitTerminalUsagePresence(ctx, s.sink, s.exchangeID, !s.latestUsage.IsZero())
	s.closeOpenText(canonical.EnvelopeStatusCompleted)
	s.closeOpenTools(canonical.EnvelopeStatusCompleted)
	s.enqueueUsage(s.latestUsage)
	s.enqueueFinish(normalizedStatus)
	s.enqueueEnvelopeEnd(s.responseEnvID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
}

func (s *responsesResponseStream) closeOpenText(status canonical.EnvelopeStatus) {
	state := s.textState
	if state == nil {
		return
	}
	if status == canonical.EnvelopeStatusCompleted {
		item, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart(state.text.String())})
		if err == nil {
			s.enqueueItemCompleted(state.envID, state.ordinal, item)
		}
	}
	s.textState = nil
}

func (s *responsesResponseStream) closeOpenTools(status canonical.EnvelopeStatus) {
	for itemID := range s.toolStates {
		delete(s.toolStates, itemID)
		delete(s.toolInputs, itemID)
	}
}

func (s *responsesResponseStream) ensureToolState(itemID string, ordinal uint32, toolType string, callID string, name string) (responsesToolState, error) {
	if state, ok := s.toolStates[itemID]; ok {
		if state.toolType == "" && strings.TrimSpace(toolType) != "" { // swobu:io-string source=domain
			state.toolType = strings.ToLower(strings.TrimSpace(toolType)) // swobu:io-string source=boundary
			s.toolStates[itemID] = state
		}
		return state, nil
	}
	normalizedType := strings.ToLower(strings.TrimSpace(toolType)) // swobu:io-string source=boundary
	if normalizedType == "" {
		normalizedType = canonical.ToolTypeFunction
	}
	if normalizedType != canonical.ToolTypeFunction && normalizedType != canonical.ToolTypeCustom {
		return responsesToolState{}, canonical.UnsupportedOperation("responses stream tool-call kind is not implemented")
	}
	resolved, _, err := canonical.ResolveToolDeclarationByName(s.request.Tools(), name, normalizedType)
	if err != nil {
		return responsesToolState{}, canonical.InternalError("responses stream tool call references an unknown or ambiguous tool")
	}
	canonicalCallID, err := canonical.NewToolCallID(callID)
	if err != nil {
		return responsesToolState{}, canonical.InternalError("responses stream tool call is missing call_id")
	}
	envID := canonical.EnvelopeID(fmt.Sprintf("%s:item:%d", s.responseEnvID, ordinal))
	state := responsesToolState{envID: envID, ordinal: ordinal, toolType: normalizedType, callID: canonicalCallID, tool: resolved.Key()}
	s.toolStates[itemID] = state
	start, err := canonical.NewToolCallStart(canonicalCallID, state.tool)
	if err != nil {
		return responsesToolState{}, err
	}
	s.enqueueItemStart(envID, ordinal, start)
	return state, nil
}

func (s *responsesResponseStream) markToolStateArgumentsDone(itemID string, state responsesToolState) responsesToolState {
	state.argumentsDone = true
	s.toolStates[itemID] = state
	return state
}

func (s *responsesResponseStream) markToolStateOutputDone(itemID string, state responsesToolState) responsesToolState {
	state.outputDone = true
	s.toolStates[itemID] = state
	return state
}

func (s *responsesResponseStream) enqueueToolArgs(itemID string, args string) {
	if args == "" { // swobu:io-string source=boundary
		return
	}
	state, ok := s.toolStates[itemID]
	if !ok {
		return
	}
	s.toolInputs[itemID] += args
	s.enqueueArgsDelta(state.envID, state.ordinal, args)
}

func fallbackItemID(itemID string, callID string, outputIndex *int) string {
	if outputIndex != nil {
		return fmt.Sprintf("tool_%d", *outputIndex)
	}
	if strings.TrimSpace(itemID) != "" { // swobu:io-string source=boundary
		return strings.TrimSpace(itemID) // swobu:io-string source=boundary
	}
	if strings.TrimSpace(callID) != "" { // swobu:io-string source=boundary
		return strings.TrimSpace(callID) // swobu:io-string source=boundary
	}
	return "tool_0"
}

func (s *responsesResponseStream) ordinalFor(itemID string, outputIndex *int) uint32 {
	if state, ok := s.toolStates[itemID]; ok {
		return state.ordinal
	}
	if outputIndex != nil && *outputIndex >= 0 {
		adjusted := int64(*outputIndex) + s.ordinalOffset
		if adjusted < 0 {
			adjusted = int64(s.nextOrdinal)
		}
		ordinal := uint32(adjusted)
		if ordinal >= s.nextOrdinal {
			s.nextOrdinal = ordinal + 1
		}
		return ordinal
	}
	ordinal := s.nextOrdinal
	s.nextOrdinal++
	return ordinal
}
