package completions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	deliverycompat "github.com/swobuforge/swobu/internal/adapters/wire/families/deliverycompat"
	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
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
	emitUsageInputTokensDecision(ctx, sink, exchangeID, usage)
	emitUsageOutputTokensDecision(ctx, sink, exchangeID, usage)
	emitUsageReasoningTokensDecision(ctx, sink, exchangeID, usage)
	emitUsageCacheReadTokensDecision(ctx, sink, exchangeID, usage)
	emitUsageCacheWriteTokensDecision(ctx, sink, exchangeID, usage)
	return canonical.NewSliceEventReader(buildBufferedResponseEvents(exchangeID, dto.ID, dto.Model, choice.Text, choice.FinishReason, usage)), nil
}

func buildBufferedResponseEvents(exchangeID, resultID, model, text, finishReason string, usage canonical.TokenUsage) []canonical.Event {
	seq := int64(0)
	nextSeq := func() int64 {
		seq++
		return seq
	}
	responseID := canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID))
	messageID := canonical.EnvelopeID(fmt.Sprintf("%s:message:0", responseID))
	return []canonical.Event{
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventEnvelopeStart,
			EnvID:      responseID,
			Payload: canonical.EnvelopeStartPayload{
				Kind: canonical.EnvResponse,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventMetadata,
			EnvID:      responseID,
			Payload: canonical.MetadataPayload{Values: map[string]string{
				"result_id": resultID,
				"model":     model,
			}},
		},
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventEnvelopeStart,
			EnvID:      messageID,
			ParentID:   responseID,
			Payload: canonical.EnvelopeStartPayload{
				Kind: canonical.EnvMessage,
				Role: canonical.ItemAuthorAssistant,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventTextDelta,
			EnvID:      messageID,
			ParentID:   responseID,
			Payload: canonical.TextDeltaPayload{
				Text: text,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventEnvelopeEnd,
			EnvID:      messageID,
			ParentID:   responseID,
			Payload: canonical.EnvelopeEndPayload{
				Kind:   canonical.EnvMessage,
				Status: canonical.EnvelopeStatusCompleted,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventUsage,
			EnvID:      responseID,
			Payload: canonical.UsagePayload{
				Usage: usage,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventFinish,
			EnvID:      responseID,
			Payload: canonical.FinishPayload{
				Reason: finishReason,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventEnvelopeEnd,
			EnvID:      responseID,
			Payload: canonical.EnvelopeEndPayload{
				Kind:   canonical.EnvResponse,
				Status: canonical.EnvelopeStatusCompleted,
			},
		},
	}
}

func emitUsageInputTokensDecision(ctx context.Context, sink effect.Sink, exchangeID string, usage canonical.TokenUsage) {
	if sink == nil {
		return
	}
	if _, ok := usage.InputTokens(); !ok {
		return
	}
	_ = sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.Compatibility{
			Feature: compat.UsageInputTokens,
			Outcome: compat.Exact,
			Subject: compat.Subject("wire:/usage/input_tokens"),
		},
	})
}

func emitUsageOutputTokensDecision(ctx context.Context, sink effect.Sink, exchangeID string, usage canonical.TokenUsage) {
	if sink == nil {
		return
	}
	if _, ok := usage.OutputTokens(); !ok {
		return
	}
	_ = sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.Compatibility{
			Feature: compat.UsageOutputTokens,
			Outcome: compat.Exact,
			Subject: compat.Subject("wire:/usage/output_tokens"),
		},
	})
}

func emitUsageReasoningTokensDecision(ctx context.Context, sink effect.Sink, exchangeID string, usage canonical.TokenUsage) {
	if sink == nil {
		return
	}
	if _, ok := usage.ReasoningTokens(); !ok {
		return
	}
	_ = sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.Compatibility{
			Feature: compat.UsageReasoningTokens,
			Outcome: compat.Exact,
			Subject: compat.Subject("wire:/usage/completion_tokens_details/reasoning_tokens"),
		},
	})
}

func emitUsageCacheReadTokensDecision(ctx context.Context, sink effect.Sink, exchangeID string, usage canonical.TokenUsage) {
	if sink == nil {
		return
	}
	if _, ok := usage.CacheReadTokens(); !ok {
		return
	}
	_ = sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.Compatibility{
			Feature: compat.UsageCacheReadTokens,
			Outcome: compat.Exact,
			Subject: compat.Subject("wire:/usage/cache_read_tokens"),
		},
	})
}

func emitUsageCacheWriteTokensDecision(ctx context.Context, sink effect.Sink, exchangeID string, usage canonical.TokenUsage) {
	if sink == nil {
		return
	}
	if _, ok := usage.CacheWriteTokens(); !ok {
		return
	}
	_ = sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.Compatibility{
			Feature: compat.UsageCacheWriteTokens,
			Outcome: compat.Exact,
			Subject: compat.Subject("wire:/usage/cache_write_tokens"),
		},
	})
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
			emitUsageInputTokensDecision(ctx, s.sink, s.exchangeID, chunkUsage)
			emitUsageOutputTokensDecision(ctx, s.sink, s.exchangeID, chunkUsage)
			emitUsageReasoningTokensDecision(ctx, s.sink, s.exchangeID, chunkUsage)
			emitUsageCacheReadTokensDecision(ctx, s.sink, s.exchangeID, chunkUsage)
			emitUsageCacheWriteTokensDecision(ctx, s.sink, s.exchangeID, chunkUsage)
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
