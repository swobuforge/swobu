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
func decodeResponseStream(stream carrier.ByteStream, exchangeID string, sink compat.Sink) *responsesEventReader {
	recording := &compat.RecordingSink{Delegate: sink}
	return &responsesEventReader{
		exchangeID:    exchangeID,
		responseEnvID: canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		sink:          recording,
		recording:     recording,
		reader:        core.NewSSEReader(stream.Body),
		toolStates:    map[string]responsesToolState{},
		toolInputs:    map[string]string{},
		textOpen:      false,
		latestUsage:   canonical.NewUnknownTokenUsage(),
	}
}

type responsesEventReader struct {
	exchangeID    string
	responseEnvID canonical.EnvelopeID
	sink          compat.Sink
	recording     *compat.RecordingSink
	reader        *core.SSEReaderCloser
	pending       canonical.EventSequence
	toolStates    map[string]responsesToolState
	toolInputs    map[string]string
	textOpen      bool
	textEnvID     canonical.EnvelopeID
	emittedOutput bool
	started       bool
	completed     bool
	latestUsage   canonical.TokenUsage
	seq           int64
}

func (s *responsesEventReader) Decisions() []compat.Decision {
	if s.recording == nil {
		return nil
	}
	return s.recording.Decisions()
}

type responsesToolState struct {
	envID         canonical.EnvelopeID
	toolType      string
	argumentsDone bool
	outputDone    bool
	closed        bool
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
	s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseEnvID, Payload: canonical.UsagePayload{Usage: usage}})
}

func (s *responsesEventReader) enqueueFinish(reason string) {
	s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseEnvID, Payload: canonical.FinishPayload{Reason: reason}})
}

func (s *responsesEventReader) enqueueMetadata(values map[string]string) {
	s.enqueue(canonical.Event{Kind: canonical.EventMetadata, EnvID: s.responseEnvID, Payload: canonical.MetadataPayload{Values: values}})
}

func (s *responsesEventReader) enqueueError(code string, message string) {
	s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseEnvID, Payload: canonical.ErrorPayload{Code: code, Message: message}})
}

func (s *responsesEventReader) handleUnexpectedEOF(ctx context.Context) {
	deliverycompat.EmitTerminalUsagePresence(ctx, s.sink, s.exchangeID, false)
	s.enqueueError("stream_unexpected_eof", "output stream ended before completed")
	s.closeOpenText(canonical.EnvelopeStatusError)
	s.closeOpenTools(canonical.EnvelopeStatusError)
	s.enqueueEnvelopeEnd(s.responseEnvID, canonical.EnvResponse, canonical.EnvelopeStatusError)
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
	deliverycompat.EmitTerminalUsagePresence(ctx, s.sink, s.exchangeID, !s.latestUsage.IsZero())
	s.closeOpenText(canonical.EnvelopeStatusCompleted)
	s.closeOpenTools(canonical.EnvelopeStatusCompleted)
	s.enqueueUsage(s.latestUsage)
	s.enqueueFinish(normalizedStatus)
	s.enqueueEnvelopeEnd(s.responseEnvID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
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
		if !state.closed {
			s.enqueueEnvelopeEnd(state.envID, canonical.EnvToolCall, status)
		}
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
	envID := canonical.EnvelopeID(fmt.Sprintf("%s:item:%s", s.responseEnvID, itemID))
	state := responsesToolState{envID: envID, toolType: normalizedType}
	s.toolStates[itemID] = state
	s.enqueueEnvelopeStart(envID, s.responseEnvID, canonical.EnvelopeStartPayload{
		Kind:      canonical.EnvToolCall,
		Name:      name,
		ToolUseID: callID,
		ToolType:  normalizedType,
	}, canonical.EventMetadataFields{NativeID: itemID})
	return state
}

func (s *responsesEventReader) markToolStateArgumentsDone(itemID string, state responsesToolState) responsesToolState {
	state.argumentsDone = true
	s.toolStates[itemID] = state
	return state
}

func (s *responsesEventReader) markToolStateOutputDone(itemID string, state responsesToolState) responsesToolState {
	state.outputDone = true
	s.toolStates[itemID] = state
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
