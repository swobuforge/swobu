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
	"github.com/swobuforge/swobu/internal/wire"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

// DecodeResponseStream returns canonical envelope events directly for responses streams.
func decodeResponseStream(request canonical.CanonicalRequest, names wire.ToolNames, stream carrier.ByteStream, exchangeID string, _ *[]compat.Change, continuationEligible bool) *responsesResponseStream {
	reader := &responsesResponseStream{
		exchangeID:           exchangeID,
		responseEnvID:        canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		reader:               core.NewSSEReader(stream.Body),
		providerOutputs:      map[int]*responsesOutputSlot{},
		latestUsage:          canonical.NewUnknownTokenUsage(),
		request:              request.Clone(),
		toolNames:            names,
		continuationEligible: continuationEligible,
	}
	reader.changeLog = &reader.changes
	return reader
}

type responsesResponseStream struct {
	exchangeID            string
	responseEnvID         canonical.EnvelopeID
	changeLog             *[]compat.Change
	changes               []compat.Change
	reader                *core.SSEReaderCloser
	pending               canonical.EventSequence
	unknownEventDecisions map[string]struct{}
	providerOutputs       map[int]*responsesOutputSlot
	outputFrontier        int
	erasedOutput          bool
	completedItems        uint32
	started               bool
	completed             bool
	latestUsage           canonical.TokenUsage
	seq                   int64
	request               canonical.CanonicalRequest
	toolNames             wire.ToolNames
	nextOrdinal           uint32
	frameIndex            int
	continuationEligible  bool
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

func (s *responsesResponseStream) Changes() []compat.Change {
	return compat.CloneChanges(s.changes)
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

func (s *responsesResponseStream) commitOutputItem(outputIndex *int, ordinal uint32, item canonical.CanonicalItem) {
	s.enqueueOutputItemCompleted(outputIndex, ordinal, item)
	s.completedItems++
}

func (s *responsesResponseStream) enqueueOutputItemCompleted(outputIndex *int, ordinal uint32, item canonical.CanonicalItem) {
	s.stageOutputEvent(outputIndex, canonical.Event{Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: ordinal}, Payload: canonical.ItemCompletedPayload{Item: item}}})
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
	if s.hasOpenKnownOutput() {
		s.discardOpenText()
		s.closeOpenTools(canonical.EnvelopeStatusError)
		s.enqueueError("stream_incomplete_output", "responses stream ended with unfinished output")
		s.enqueueEnvelopeEnd(s.responseEnvID, canonical.EnvResponse, canonical.EnvelopeStatusError)
		return nil
	}
	if s.completedItems == 0 && s.erasedOutput {
		return canonical.NewBackendError("responses", 0, "backend produced no usable canonical output", "")
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
			state.phase != responsesOutputDone || !state.published || len(state.events) > 0 {
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
	key, err := wire.DecodeToolKey(s.toolNames, environment, canonical.ToolKind(normalizedType), name)
	if err != nil {
		return nil, canonical.InternalError("responses stream tool call references an unknown or ambiguous tool")
	}
	canonicalCallID, err := canonical.NewToolCallID(callID)
	if err != nil {
		return nil, canonical.InternalError("responses stream tool call is missing call_id")
	}
	state := &responsesToolState{ordinal: ordinal, toolType: normalizedType, callID: canonicalCallID, tool: key}
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
	s.markOutputDone(frame.OutputIndex)
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
	return appendResponsesOccurrenceChange(
		s.changeLog,
		s.exchangeID,
		canonical.ResponseItemsKind,
		compat.Omission,
		canonical.ResponseItemOccurrence(uint32(*frame.OutputIndex)),
	)
}

// classifyErasedProviderOutput records one indexed additive erasure without
// closing the lifecycle. Unknown fragmented items remain open until their item
// completion or terminal snapshot arrives.
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
	return appendResponsesOccurrenceChange(s.changeLog, s.exchangeID, canonical.ResponseItemsKind, compat.Omission, canonical.ResponseItemOccurrence(uint32(index)))
}
