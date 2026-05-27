package completions

import (
	"encoding/json"
	"io"
	"strings"

	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	streamwire "github.com/swobuforge/swobu/internal/adapters/wire/shared/streamwire"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type CompletionsFamilyCodec struct{}

func (CompletionsFamilyCodec) DecodeRequest(raw []byte) (canonical.CanonicalRequest, bool, error) {
	var dto completionsRequestDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return canonical.CanonicalRequest{}, false, canonical.BadRequest("completions request body is invalid JSON")
	}
	if dto.Prompt == "" {
		return canonical.CanonicalRequest{}, false, canonical.BadRequest("completions request is missing required fields")
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:     strings.TrimSpace(dto.Model), // swobu:io-string source=boundary
		InputText: dto.Prompt,
	}), dto.Stream, nil
}

func (CompletionsFamilyCodec) EncodeResponse(output canonical.CanonicalOutput) ([]byte, error) {
	return json.Marshal(completionsResponseDTO{
		ID:     streamwire.FallbackID(output.ResultID(), "cmpl_swobu"),
		Object: "text_completion",
		Model:  output.Model(),
		Choices: []completionsChoiceDTO{{
			Index:        0,
			Text:         streamwire.OutputText(output.Items()),
			FinishReason: streamwire.DefaultFinishReason(output.FinishReason(), "stop"),
		}},
		Usage: completionsUsageFromCanonical(output.Usage()),
	})
}

func (CompletionsFamilyCodec) EncodeRequest(request canonical.CanonicalRequest, streaming bool) (core.WireRequest, error) {
	return encodeRequest(request, streaming)
}

func (CompletionsFamilyCodec) DecodeResponse(raw []byte) (canonical.CanonicalOutputValue, error) {
	return decodeResponseBuffered(raw)
}

func (CompletionsFamilyCodec) DecodeResponseStream(body io.ReadCloser, exchangeID string) canonical.EventReader {
	return decodeResponseStream(body, exchangeID)
}

func (CompletionsFamilyCodec) NewStreamState() streamwire.EnvelopeStreamEncoder {
	return &completionsEnvelopeStreamEncoder{adapter: streamwire.NewEnvelopeEventAdapter()}
}

type completionsEnvelopeStreamEncoder struct {
	adapter *streamwire.EnvelopeEventAdapter
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

func (s *completionsEnvelopeStreamEncoder) Encode(event streamwire.StreamEvent) ([][]byte, error) {
	switch event.Kind {
	case streamwire.StreamEventStarted, streamwire.StreamEventItemStarted, streamwire.StreamEventItemCompleted:
		return nil, nil
	case streamwire.StreamEventTextDelta:
		raw, _ := json.Marshal(completionsChunkDTO{
			Object: "text_completion",
			Choices: []completionsChoiceDTO{{
				Index: 0,
				Text:  event.TextDelta,
			}},
		})
		return [][]byte{streamwire.SSEData(raw)}, nil
	case streamwire.StreamEventCompleted:
		raw, _ := json.Marshal(completionsChunkDTO{
			Object: "text_completion",
			Choices: []completionsChoiceDTO{{
				Index:        0,
				Text:         "",
				FinishReason: streamwire.DefaultFinishReason(event.FinishReason, "stop"),
			}},
			Usage: completionsUsageFromCanonical(event.Usage),
		})
		return [][]byte{
			streamwire.SSEData(raw),
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
