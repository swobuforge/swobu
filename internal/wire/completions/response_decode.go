package completions

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
	"github.com/swobuforge/swobu/internal/effect"
	deliverycompat "github.com/swobuforge/swobu/internal/wire/deliverycompat"
	openaicompat "github.com/swobuforge/swobu/internal/wire/openai"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

type responseBody struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Text         string `json:"text"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
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

func decodeResponseBuffered(ctx context.Context, raw []byte, exchangeID string, sink effect.Sink) (canonical.EventReader, error) {
	var dto responseBody
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, canonical.InternalError("completions response is invalid JSON")
	}
	if len(dto.Choices) == 0 {
		return nil, canonical.InternalError("completions response is missing choices")
	}
	choice := dto.Choices[0]
	// Buffered decode emits canonical envelope events directly so the success
	// path does not depend on the output-to-event projection bridge.
	usage := core.ExtractTokenUsage(raw, tokenUsagePathSpec)
	_, inputPresent := usage.InputTokens()
	openaicompat.EmitUsageCompatibilityEffect(ctx, sink, exchangeID, inputPresent, compat.UsageInputTokens, compat.Subject("wire:/usage/input_tokens"))
	_, outputPresent := usage.OutputTokens()
	openaicompat.EmitUsageCompatibilityEffect(ctx, sink, exchangeID, outputPresent, compat.UsageOutputTokens, compat.Subject("wire:/usage/output_tokens"))
	_, reasoningPresent := usage.ReasoningTokens()
	openaicompat.EmitUsageCompatibilityEffect(ctx, sink, exchangeID, reasoningPresent, compat.UsageReasoningTokens, compat.Subject("wire:/usage/completion_tokens_details/reasoning_tokens"))
	_, cacheReadPresent := usage.CacheReadTokens()
	openaicompat.EmitUsageCompatibilityEffect(ctx, sink, exchangeID, cacheReadPresent, compat.UsageCacheReadTokens, compat.Subject("wire:/usage/cache_read_tokens"))
	_, cacheWritePresent := usage.CacheWriteTokens()
	openaicompat.EmitUsageCompatibilityEffect(ctx, sink, exchangeID, cacheWritePresent, compat.UsageCacheWriteTokens, compat.Subject("wire:/usage/cache_write_tokens"))
	return canonical.NewSliceEventReader(buildBufferedResponseEvents(exchangeID, dto.ID, dto.Model, choice.Text, choice.FinishReason, usage)), nil
}

// DecodeResponseStream returns canonical envelope events directly for completions streams.
func decodeResponseStream(stream carrier.WireStream, exchangeID string, sink effect.Sink) canonical.EventReader {
	recording := &effect.RecordingSink{Delegate: sink}
	return &completionsEventReader{
		exchangeID: exchangeID,
		responseID: canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		sink:       recording,
		recording:  recording,
		reader:     core.NewSSEReader(carrier.ReadCloserFromFrameReader(stream.Frames)),
	}
}

type completionsEventReader struct {
	exchangeID string
	responseID canonical.EnvelopeID
	sink       effect.Sink
	recording  *effect.RecordingSink
	reader     *core.SSEReaderCloser
	started    bool
	textOpen   bool
	textEnvID  canonical.EnvelopeID
	pending    canonical.EventSequence
	resultID   string
	model      string
	completed  bool
	usage      canonical.TokenUsage
	seq        int64
}

func (s *completionsEventReader) Effects() []effect.Effect {
	if s.recording == nil {
		return nil
	}
	return append([]effect.Effect(nil), s.recording.Effects...)
}

// variants while maintaining canonical output ordering.
func (s *completionsEventReader) Next(ctx context.Context) (canonical.Event, error) {
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		return event, nil
	}
	for {
		event, err := s.reader.Next()
		if err != nil {
			if err == io.EOF && s.started && !s.completed {
				deliverycompat.EmitTerminalEventDecision(ctx, s.sink, s.exchangeID, false)
				s.enqueueError("stream_unexpected_eof", "output stream ended before completed")
				s.closeOpenTextWithStatus(canonical.EnvelopeStatusError)
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
			s.usage = chunkUsage
			_, inputPresent := chunkUsage.InputTokens()
			openaicompat.EmitUsageCompatibilityEffect(ctx, s.sink, s.exchangeID, inputPresent, compat.UsageInputTokens, compat.Subject("wire:/usage/input_tokens"))
			_, outputPresent := chunkUsage.OutputTokens()
			openaicompat.EmitUsageCompatibilityEffect(ctx, s.sink, s.exchangeID, outputPresent, compat.UsageOutputTokens, compat.Subject("wire:/usage/output_tokens"))
			_, reasoningPresent := chunkUsage.ReasoningTokens()
			openaicompat.EmitUsageCompatibilityEffect(ctx, s.sink, s.exchangeID, reasoningPresent, compat.UsageReasoningTokens, compat.Subject("wire:/usage/completion_tokens_details/reasoning_tokens"))
			_, cacheReadPresent := chunkUsage.CacheReadTokens()
			openaicompat.EmitUsageCompatibilityEffect(ctx, s.sink, s.exchangeID, cacheReadPresent, compat.UsageCacheReadTokens, compat.Subject("wire:/usage/cache_read_tokens"))
			_, cacheWritePresent := chunkUsage.CacheWriteTokens()
			openaicompat.EmitUsageCompatibilityEffect(ctx, s.sink, s.exchangeID, cacheWritePresent, compat.UsageCacheWriteTokens, compat.Subject("wire:/usage/cache_write_tokens"))
		}
		var chunk responseBody
		if err := json.Unmarshal(rawChunk, &chunk); err != nil {
			return canonical.Event{}, canonical.InternalError("completions stream chunk is invalid JSON")
		}
		if !s.started {
			s.started = true
			s.resultID = chunk.ID
			s.model = chunk.Model
			s.enqueueEnvelopeStart(s.responseID, "", canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse})
			s.enqueueMetadata(map[string]string{"result_id": chunk.ID, "model": chunk.Model})
			s.usage = canonical.NewUnknownTokenUsage()
			if !chunkUsage.IsZero() {
				s.usage = chunkUsage
			}
			out := s.pending[0]
			s.pending = s.pending[1:]
			return out, nil
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.Text != "" {
			if !s.textOpen {
				s.textOpen = true
				s.textEnvID = canonical.EnvelopeID(fmt.Sprintf("%s:item:text_0", s.responseID))
				s.enqueueEnvelopeStart(s.textEnvID, s.responseID, canonical.EnvelopeStartPayload{
					Kind: canonical.EnvMessage,
					Role: canonical.ItemAuthorAssistant,
				}, canonical.EventMetadataFields{NativeID: "text_0"})
			}
			s.enqueueTextDelta(s.textEnvID, choice.Text)
			event := s.pending[0]
			s.pending = s.pending[1:]
			return event, nil
		}
		if strings.TrimSpace(choice.FinishReason) != "" && !s.completed { // swobu:io-string source=boundary
			s.completed = true
			deliverycompat.EmitTerminalEventDecision(ctx, s.sink, s.exchangeID, !s.usage.IsZero())
			s.closeOpenTextWithStatus(canonical.EnvelopeStatusCompleted)
			s.enqueueUsage(s.usage)
			s.enqueueFinish(choice.FinishReason)
			s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
			event := s.pending[0]
			s.pending = s.pending[1:]
			return event, nil
		}
	}
}

func (s *completionsEventReader) Close(context.Context) error {
	return s.reader.Close()
}

func (s *completionsEventReader) nextSeq() int64 {
	s.seq++
	return s.seq
}

func (s *completionsEventReader) enqueue(ev canonical.Event) {
	ev.ExchangeID = s.exchangeID
	ev.Seq = s.nextSeq()
	ev.Time = time.Now().UTC()
	s.pending = append(s.pending, ev)
}

func (s *completionsEventReader) enqueueEnvelopeStart(id canonical.EnvelopeID, parent canonical.EnvelopeID, payload canonical.EnvelopeStartPayload, meta ...canonical.EventMetadataFields) {
	ev := canonical.Event{
		Kind:     canonical.EventEnvelopeStart,
		EnvID:    id,
		ParentID: parent,
		Payload:  payload,
	}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	s.enqueue(ev)
}

func (s *completionsEventReader) enqueueEnvelopeEnd(id canonical.EnvelopeID, kind canonical.EnvelopeKind, status canonical.EnvelopeStatus) {
	s.enqueue(canonical.Event{
		Kind:  canonical.EventEnvelopeEnd,
		EnvID: id,
		Payload: canonical.EnvelopeEndPayload{
			Kind:   kind,
			Status: status,
		},
	})
}

func (s *completionsEventReader) enqueueTextDelta(id canonical.EnvelopeID, text string) {
	s.enqueue(canonical.Event{
		Kind:    canonical.EventTextDelta,
		EnvID:   id,
		Payload: canonical.TextDeltaPayload{Text: text},
	})
}

func (s *completionsEventReader) enqueueUsage(usage canonical.TokenUsage) {
	s.enqueue(canonical.Event{
		Kind:    canonical.EventUsage,
		EnvID:   s.responseID,
		Payload: canonical.UsagePayload{Usage: usage},
	})
}

func (s *completionsEventReader) enqueueFinish(reason string) {
	s.enqueue(canonical.Event{
		Kind:    canonical.EventFinish,
		EnvID:   s.responseID,
		Payload: canonical.FinishPayload{Reason: reason},
	})
}

func (s *completionsEventReader) enqueueMetadata(values map[string]string) {
	s.enqueue(canonical.Event{
		Kind:    canonical.EventMetadata,
		EnvID:   s.responseID,
		Payload: canonical.MetadataPayload{Values: values},
	})
}

func (s *completionsEventReader) enqueueError(code string, message string) {
	s.enqueue(canonical.Event{
		Kind:  canonical.EventError,
		EnvID: s.responseID,
		Payload: canonical.ErrorPayload{
			Code:    code,
			Message: message,
		},
	})
}

func (s *completionsEventReader) closeOpenTextWithStatus(status canonical.EnvelopeStatus) {
	if s.textOpen {
		s.enqueueEnvelopeEnd(s.textEnvID, canonical.EnvMessage, status)
		s.textOpen = false
		s.textEnvID = ""
	}
}
