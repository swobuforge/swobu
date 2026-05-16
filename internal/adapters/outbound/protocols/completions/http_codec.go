package completions

import (
	"encoding/json"
	"strings"

	httpcodec "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/httpcodec"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type CompletionsCodec struct{}

func (CompletionsCodec) DecodeRequest(raw []byte) (canonical.CanonicalRequest, bool, error) {
	var dto completionsRequestDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, false, canonical.BadRequest("completions request body is invalid JSON")
	}
	if dto.Prompt == "" {
		return nil, false, canonical.BadRequest("completions request is missing required fields")
	}
	return canonical.NewPromptRequest(strings.TrimSpace(dto.Model), dto.Prompt), dto.Stream, nil // trimlowerlint:allow boundary canonicalization
}

func (CompletionsCodec) EncodeBuffered(output canonical.CanonicalOutput) ([]byte, error) {
	return json.Marshal(completionsResponseDTO{
		ID:     httpcodec.FallbackID(output.ResultID(), "cmpl_swobu"),
		Object: "text_completion",
		Model:  output.Model(),
		Choices: []completionsChoiceDTO{{
			Index:        0,
			Text:         httpcodec.OutputText(output.Items()),
			FinishReason: httpcodec.DefaultFinishReason(output.FinishReason(), "stop"),
		}},
		Usage: completionsUsageFromCanonical(output.Usage()),
	})
}

func (CompletionsCodec) NewStreamState() httpcodec.EnvelopeStreamEncoder {
	return &completionsClientStreamEncoder{adapter: httpcodec.NewEnvelopeEventAdapter()}
}

type completionsClientStreamEncoder struct {
	adapter *httpcodec.EnvelopeEventAdapter
}

func (s *completionsClientStreamEncoder) EncodeEnvelopeEvent(event canonical.Event) ([][]byte, error) {
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

func (s *completionsClientStreamEncoder) Encode(event httpcodec.StreamEvent) ([][]byte, error) {
	switch event.Kind {
	case httpcodec.StreamEventStarted, httpcodec.StreamEventItemStarted, httpcodec.StreamEventItemCompleted:
		return nil, nil
	case httpcodec.StreamEventTextDelta:
		raw, _ := json.Marshal(completionsChunkDTO{
			Object: "text_completion",
			Choices: []completionsChoiceDTO{{
				Index: 0,
				Text:  event.TextDelta,
			}},
		})
		return [][]byte{httpcodec.SSEData(raw)}, nil
	case httpcodec.StreamEventCompleted:
		raw, _ := json.Marshal(completionsChunkDTO{
			Object: "text_completion",
			Choices: []completionsChoiceDTO{{
				Index:        0,
				Text:         "",
				FinishReason: httpcodec.DefaultFinishReason(event.FinishReason, "stop"),
			}},
			Usage: completionsUsageFromCanonical(event.Usage),
		})
		return [][]byte{
			httpcodec.SSEData(raw),
			[]byte("data: [DONE]\n\n"),
		}, nil
	default:
		return nil, canonical.UnsupportedOperation("completions streaming event is not implemented")
	}
}

func (s *completionsClientStreamEncoder) Finish() ([][]byte, error) { return nil, nil }

func completionsUsageFromCanonical(usage canonical.TokenUsage) *completionsUsageDTO {
	input, hasInput := usage.InputTokens()
	output, hasOutput := usage.OutputTokens()
	cacheRead, hasCacheRead := usage.CacheReadTokens()
	cacheWrite, hasCacheWrite := usage.CacheWriteTokens()
	if !hasInput && !hasOutput && !hasCacheRead && !hasCacheWrite {
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
	return dto
}
