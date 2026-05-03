package httpapi

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

type completionsFamilyCodec struct{}

func (completionsFamilyCodec) decodeRequest(raw []byte) (compatibility.CanonicalRequest, compatibility.DeliveryMode, error) {
	var dto completionsRequestDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, "", compatibility.BadRequest("completions request body is invalid JSON")
	}
	if dto.Prompt == "" {
		return nil, "", compatibility.BadRequest("completions request is missing required fields")
	}
	return compatibility.NewPromptRequest(strings.TrimSpace(dto.Model), dto.Prompt), deliveryModeFromStream(dto.Stream), nil
}

func (completionsFamilyCodec) encodeBuffered(output compatibility.CanonicalOutput) ([]byte, error) {
	return json.Marshal(completionsResponseDTO{
		ID:     fallbackID(output.ResultID(), "cmpl_swobu"),
		Object: "text_completion",
		Model:  output.Model(),
		Choices: []completionsChoiceDTO{{
			Index:        0,
			Text:         outputText(output.Items()),
			FinishReason: defaultFinishReason(output.FinishReason(), "stop"),
		}},
		Usage: completionsUsageFromCanonical(output.Usage()),
	})
}

func (completionsFamilyCodec) newStreamState() clientStreamEncoder {
	return &completionsClientStreamEncoder{}
}

type completionsClientStreamEncoder struct{}

func (s *completionsClientStreamEncoder) Encode(event compatibility.OutputEvent) ([][]byte, error) {
	switch event.Kind {
	case compatibility.OutputEventStarted, compatibility.OutputEventItemStarted, compatibility.OutputEventItemCompleted:
		return nil, nil
	case compatibility.OutputEventTextDelta:
		raw, _ := json.Marshal(completionsChunkDTO{
			Object: "text_completion",
			Choices: []completionsChoiceDTO{{
				Index: 0,
				Text:  event.TextDelta,
			}},
		})
		return [][]byte{sseData(raw)}, nil
	case compatibility.OutputEventCompleted:
		raw, _ := json.Marshal(completionsChunkDTO{
			Object: "text_completion",
			Choices: []completionsChoiceDTO{{
				Index:        0,
				Text:         "",
				FinishReason: defaultFinishReason(event.FinishReason, "stop"),
			}},
			Usage: completionsUsageFromCanonical(event.Usage),
		})
		return [][]byte{
			sseData(raw),
			[]byte("data: [DONE]\n\n"),
		}, nil
	default:
		return nil, compatibility.UnsupportedOperation("completions streaming event is not implemented")
	}
}

func (s *completionsClientStreamEncoder) Finish() ([][]byte, error) { return nil, nil }
