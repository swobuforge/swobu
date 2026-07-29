package responses

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

// DecodeResponseStream returns canonical envelope events directly for responses streams.
func decodeResponseStream(request canonical.CanonicalRequest, stream carrier.ByteStream, exchangeID string, sink compat.Sink) *responsesResponseStream {
	recording := &compat.RecordingSink{Delegate: sink}
	streamSink := &responsesStreamDecisionSink{
		recording: recording,
		seen:      make(map[responsesStreamDecisionKey]struct{}),
	}
	return &responsesResponseStream{
		exchangeID:      exchangeID,
		responseEnvID:   canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		sink:            streamSink,
		recording:       recording,
		reader:          core.NewSSEReader(stream.Body),
		providerOutputs: map[int]*pendingResponseOutput{},
		latestUsage:     canonical.NewUnknownTokenUsage(),
		request:         request.Clone(),
	}
}

type responsesStreamDecisionKey struct {
	feature compat.Feature
	outcome compat.Outcome
	subject compat.Subject
}

// responsesStreamDecisionSink records each semantic compatibility occurrence
// once even when output_item.done and response.completed repeat a checkpoint.
// A terminal-only child has a new subject and therefore remains observable.
type responsesStreamDecisionSink struct {
	recording *compat.RecordingSink
	seen      map[responsesStreamDecisionKey]struct{}
}

func (s *responsesStreamDecisionSink) Commit(ctx context.Context, exchangeID string, decisions []compat.Decision) error {
	if s == nil || s.recording == nil {
		return nil
	}
	fresh := make([]compat.Decision, 0, len(decisions))
	for _, decision := range decisions {
		key := responsesStreamDecisionKey{
			feature: decision.Feature,
			outcome: decision.Outcome,
			subject: decision.Subject,
		}
		if _, exists := s.seen[key]; exists {
			continue
		}
		s.seen[key] = struct{}{}
		fresh = append(fresh, decision)
	}
	if len(fresh) == 0 {
		return nil
	}
	return s.recording.Commit(ctx, exchangeID, fresh)
}

type responsesResponseStream struct {
	exchangeID            string
	responseEnvID         canonical.EnvelopeID
	sink                  compat.Sink
	recording             *compat.RecordingSink
	reader                *core.SSEReaderCloser
	pending               canonical.EventSequence
	unknownEventDecisions map[string]struct{}
	providerOutputs       map[int]*pendingResponseOutput
	outputFrontier        int
	erasedOutput          bool
	completedItems        uint32
	started               bool
	completed             bool
	latestUsage           canonical.TokenUsage
	seq                   int64
	request               canonical.CanonicalRequest
	nextOrdinal           uint32
	frameIndex            int
}

func (s *responsesResponseStream) unresolvedTerminalOutputs(items []json.RawMessage) ([]json.RawMessage, []int) {
	output := make([]json.RawMessage, 0, len(items))
	indexes := make([]int, 0, len(items))
	for index, item := range items {
		if s.isOutputComplete(index) {
			continue
		}
		output = append(output, item)
		indexes = append(indexes, index)
	}
	return output, indexes
}

type responsesReasoningStreamPartState struct {
	text strings.Builder
}

type responsesReasoningState struct {
	summaryParts map[int]*responsesReasoningStreamPartState
	traceParts   map[int]*responsesReasoningStreamPartState
}

func newResponsesReasoningState() *responsesReasoningState {
	return &responsesReasoningState{
		summaryParts: make(map[int]*responsesReasoningStreamPartState),
		traceParts:   make(map[int]*responsesReasoningStreamPartState),
	}
}

// responsesTextState owns one indexed provider message and its nested
// content-index frontier. Parts are classified independently, but canonical
// part ordinals are assigned only while flushing the contiguous wire prefix.
type responsesTextState struct {
	ordinal         uint32
	parts           map[int]*responsesTextPartState
	partFrontier    int
	nextPartOrdinal uint32
}

type responsesTextPartState struct {
	classified bool
	erased     bool
	emitted    bool
	ordinal    uint32
	text       strings.Builder
	deltas     []string
}

func newResponsesTextState(ordinal uint32) *responsesTextState {
	return &responsesTextState{ordinal: ordinal, parts: make(map[int]*responsesTextPartState)}
}

func (s *responsesResponseStream) Decisions() []compat.Decision {
	if s.recording == nil {
		return nil
	}
	return s.recording.Decisions()
}

type responsesToolState struct {
	ordinal       uint32
	toolType      string
	callID        canonical.ToolCallID
	tool          canonical.ToolKey
	input         string
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
				if err := s.handleStreamDone(ctx); err != nil {
					return canonical.Event{}, err
				}
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
			return canonical.Event{}, canonical.InternalError("responses stream completed item is invalid JSON")
		}
		frame.RawItem = native.Item
		frame.RawOutput = native.Response.Output
		frame.EventIndex = s.frameIndex
		s.frameIndex++
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

func (s *responsesResponseStream) enqueueContentStart(outputIndex *int, ordinal uint32, part uint32) {
	s.stageOutputEvent(outputIndex, canonical.Event{Kind: canonical.EventContentStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: ordinal, Part: part}, Payload: canonical.NewMessageContentStart(canonical.PartKindText)}})
}

func (s *responsesResponseStream) enqueueTextDelta(outputIndex *int, ordinal uint32, part uint32, text string) {
	s.stageOutputEvent(outputIndex, canonical.Event{Kind: canonical.EventTextDelta, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: ordinal, Part: part}, Payload: canonical.TextDeltaPayload{Text: text}}})
}

func (s *responsesResponseStream) enqueueArgsDelta(outputIndex *int, ordinal uint32, args string) {
	s.stageOutputEvent(outputIndex, canonical.Event{Kind: canonical.EventArgsDelta, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: ordinal}, Payload: canonical.ArgsDeltaPayload{Args: args}}})
}

func (s *responsesResponseStream) enqueueItemStart(outputIndex *int, ordinal uint32, start canonical.ItemStartPayload) {
	s.stageOutputEvent(outputIndex, canonical.Event{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: ordinal}, Payload: start}})
}

func (s *responsesResponseStream) enqueueItemCompleted(outputIndex *int, ordinal uint32, item canonical.CanonicalItem) {
	if outputIndex != nil && *outputIndex >= 0 {
		output := s.outputAt(*outputIndex)
		output.checkpointItems = append(output.checkpointItems, item.Clone())
	}
	s.stageOutputEvent(outputIndex, canonical.Event{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: ordinal}, Payload: canonical.ItemCompletedPayload{Item: item}}})
	s.completedItems++
}

func (s *responsesResponseStream) enqueueUsage(usage canonical.TokenUsage) {
	s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseEnvID, Payload: canonical.UsagePayload{Usage: usage}})
}

func (s *responsesResponseStream) enqueueFinish(completion canonical.Completion) {
	s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseEnvID, Payload: canonical.FinishPayload{Completion: completion}})
}

func (s *responsesResponseStream) enqueueError(code string, message string) {
	s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseEnvID, Payload: canonical.ErrorPayload{Code: code, Message: message}})
}

func (s *responsesResponseStream) handleUnexpectedEOF(ctx context.Context) {
	deliverycompat.EmitTerminalUsagePresence(ctx, s.sink, s.exchangeID, false)
	s.enqueueError("stream_unexpected_eof", "output stream ended before completed")
	s.discardOpenText()
	s.closeOpenTools(canonical.EnvelopeStatusError)
	s.enqueueEnvelopeEnd(s.responseEnvID, canonical.EnvResponse, canonical.EnvelopeStatusError)
	s.completed = true
}

func (s *responsesResponseStream) handleStreamDone(ctx context.Context) error {
	return s.handleTerminalCompletion(ctx, "completed")
}

func (s *responsesResponseStream) handleTerminalCompletion(ctx context.Context, status string) error {
	normalizedStatus := strings.TrimSpace(status) // swobu:io-string source=provider-wire
	if normalizedStatus == "" {
		normalizedStatus = "completed"
	}
	s.completed = true
	deliverycompat.EmitTerminalUsagePresence(ctx, s.sink, s.exchangeID, !s.latestUsage.IsZero())
	if s.hasOpenKnownOutput() {
		s.discardOpenText()
		s.closeOpenTools(canonical.EnvelopeStatusError)
		s.enqueueError("stream_incomplete_output", "responses stream ended with unfinished output")
		s.enqueueEnvelopeEnd(s.responseEnvID, canonical.EnvResponse, canonical.EnvelopeStatusError)
		return nil
	}
	if s.completedItems == 0 && s.erasedOutput {
		return canonical.NewBackendError("responses", 0, "responses output has no surviving semantic items", "")
	}
	s.closeOpenTools(canonical.EnvelopeStatusCompleted)
	s.enqueueUsage(s.latestUsage)
	s.enqueueFinish(responsesCompletion(normalizedStatus, normalizedStatus))
	s.enqueueEnvelopeEnd(s.responseEnvID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
	return nil
}

func (s *responsesResponseStream) discardOpenText() {
	for _, output := range s.providerOutputs {
		output.text = nil
	}
}

func (s *responsesResponseStream) hasOpenKnownOutput() bool {
	for _, state := range s.providerOutputs {
		if state.text != nil || state.tool != nil || state.reasoning != nil ||
			!state.resolved || !state.active || len(state.events) > 0 {
			return true
		}
	}
	return false
}

func (s *responsesResponseStream) closeOpenTools(status canonical.EnvelopeStatus) {
	for _, output := range s.providerOutputs {
		output.tool = nil
	}
}

func (s *responsesResponseStream) ensureToolState(outputIndex int, ordinal uint32, toolType string, callID string, name string) (*responsesToolState, error) {
	output := s.outputAt(outputIndex)
	if state := output.tool; state != nil {
		if state.toolType == "" && strings.TrimSpace(toolType) != "" { // swobu:io-string source=domain
			state.toolType = strings.ToLower(strings.TrimSpace(toolType)) // swobu:io-string source=boundary
		}
		return state, nil
	}
	normalizedType := strings.ToLower(strings.TrimSpace(toolType)) // swobu:io-string source=boundary
	if normalizedType == "" {
		normalizedType = canonical.ToolTypeFunction
	}
	if normalizedType != canonical.ToolTypeFunction && normalizedType != canonical.ToolTypeCustom {
		return nil, canonical.InternalError("Responses stream dispatch admitted an unknown canonical tool-call kind")
	}
	environment, err := canonical.EffectiveTools(s.request)
	if err != nil {
		return nil, canonical.InternalError("responses stream tool environment is ambiguous")
	}
	resolved, _, err := canonical.ResolveToolDeclarationByName(environment.Declarations(), name, normalizedType)
	if err != nil {
		return nil, canonical.InternalError("responses stream tool call references an unknown or ambiguous tool")
	}
	canonicalCallID, err := canonical.NewToolCallID(callID)
	if err != nil {
		return nil, canonical.InternalError("responses stream tool call is missing call_id")
	}
	state := &responsesToolState{ordinal: ordinal, toolType: normalizedType, callID: canonicalCallID, tool: resolved.Key()}
	output.tool = state
	start, err := canonical.NewToolCallStart(canonicalCallID, state.tool)
	if err != nil {
		return nil, err
	}
	s.enqueueItemStart(&outputIndex, ordinal, start)
	return state, nil
}

func (s *responsesResponseStream) markToolStateArgumentsDone(state *responsesToolState) {
	state.argumentsDone = true
}

func (s *responsesResponseStream) markToolStateOutputDone(state *responsesToolState) {
	state.outputDone = true
}

func (s *responsesResponseStream) enqueueToolArgs(outputIndex int, args string) {
	if args == "" { // swobu:io-string source=boundary
		return
	}
	state := s.outputAt(outputIndex).tool
	if state == nil {
		return
	}
	state.input += args
	s.enqueueArgsDelta(&outputIndex, state.ordinal, args)
}

func (s *responsesResponseStream) omitProviderOutput(outputIndex *int) {
	s.dropOutput(outputIndex)
}

func (s *responsesResponseStream) eraseProviderOutput(frame streamFrame, field string) error {
	if err := s.classifyErasedProviderOutput(frame, field); err != nil {
		return err
	}
	s.completeOutput(frame.OutputIndex)
	return nil
}

func (s *responsesResponseStream) recordUnknownWebSearchStatus(frame streamFrame) error {
	if frame.OutputIndex == nil {
		return canonical.NewBackendError("responses", 0, "responses web-search lifecycle is missing output index", "")
	}
	state := s.outputAt(*frame.OutputIndex)
	if state.statusDropRecorded {
		return nil
	}
	state.statusDropRecorded = true
	return emitResponsesCompatibilityDecision(
		s.sink,
		s.exchangeID,
		compat.ResponseItemsKind,
		compat.Drop,
		compat.Subject(fmt.Sprintf("wire:/output/%d/status", *frame.OutputIndex)),
	)
}

// classifyErasedProviderOutput records one indexed additive erasure without
// closing the lifecycle. Unknown fragmented items remain open until their item
// checkpoint or terminal snapshot arrives.
func (s *responsesResponseStream) classifyErasedProviderOutput(frame streamFrame, field string) error {
	s.erasedOutput = true
	s.omitProviderOutput(frame.OutputIndex)
	if frame.OutputIndex == nil {
		return canonical.NewBackendError("responses", 0, "responses output lifecycle is missing output index", "")
	}
	state := s.outputAt(*frame.OutputIndex)
	if state.erasureRecorded {
		return nil
	}
	state.erasureRecorded = true
	index := *frame.OutputIndex
	return emitResponsesCompatibilityDecision(s.sink, s.exchangeID, compat.ResponseItemsKind, compat.Drop, compat.Subject(fmt.Sprintf("wire:/output/%d/%s", index, field)))
}
