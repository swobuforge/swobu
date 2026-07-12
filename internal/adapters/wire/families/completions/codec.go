package completions

import (
	"encoding/json"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type completionsEnvelopeStreamEncoder struct {
	adapter *sse.EnvelopeEventAdapter
}

func (s *completionsEnvelopeStreamEncoder) EncodeEnvelopeEvent(event canonical.Event) ([][]byte, error) {
	streamEvents := s.adapter.Translate(event)
	frames := make([][]byte, 0, len(streamEvents))
	for _, streamEvent := range streamEvents {
		emitted, err := s.Encode(streamEvent)
		if err != nil {
			return nil, err
		}
		frames = append(frames, emitted...)
	}
	return frames, nil
}

func (s *completionsEnvelopeStreamEncoder) Encode(event sse.StreamEvent) ([][]byte, error) {
	switch event.Kind {
	case sse.StreamEventStarted, sse.StreamEventItemStarted, sse.StreamEventItemCompleted:
		return nil, nil
	case sse.StreamEventTextDelta:
		raw, _ := json.Marshal(completionsChunkDTO{
			Object: "text_completion",
			Choices: []completionsChoiceDTO{{
				Index: 0,
				Text:  event.TextDelta,
			}},
		})
		return [][]byte{sse.SSEData(raw)}, nil
	case sse.StreamEventCompleted:
		raw, _ := json.Marshal(completionsChunkDTO{
			Object: "text_completion",
			Choices: []completionsChoiceDTO{{
				Index:        0,
				Text:         "",
				FinishReason: sse.DefaultFinishReason(event.FinishReason, "stop"),
			}},
			Usage: completionsUsageFromCanonical(event.Usage),
		})
		return [][]byte{
			sse.SSEData(raw),
			[]byte("data: [DONE]\n\n"),
		}, nil
	default:
		return nil, canonical.UnsupportedOperation("completions streaming event is not implemented")
	}
}

func (s *completionsEnvelopeStreamEncoder) Finish() ([][]byte, error) { return nil, nil }

func completionsUsageFromCanonical(usage canonical.TokenUsage) *completionsUsageDTO {
	input, hasInput := usage.InputTokens()
	output, hasOutput := usage.OutputTokens()
	reasoning, hasReasoning := usage.ReasoningTokens()
	cacheRead, hasCacheRead := usage.CacheReadTokens()
	cacheWrite, hasCacheWrite := usage.CacheWriteTokens()
	if !hasInput && !hasOutput && !hasReasoning && !hasCacheRead && !hasCacheWrite {
		return nil
	}
	dto := &completionsUsageDTO{
		PromptTokens:     input,
		CompletionTokens: output,
		TotalTokens:      input + output,
	}
	if hasCacheRead || hasCacheWrite {
		dto.PromptDetails = &completionsPromptUsageDTO{
			CachedTokens:     cacheRead,
			CacheWriteTokens: cacheWrite,
		}
	}
	if hasReasoning {
		// Preserve provider-reported reasoning usage as a separate accounting
		// fact; do not fold it into total_tokens or drop a zero value.
		dto.CompletionDetails = &completionsCompletionTokenDetailsDTO{
			ReasoningTokens: reasoning,
		}
	}
	return dto
}
