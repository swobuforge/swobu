package completions

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalOutput) (effect.Result[carrier.CarrierDocument], error) {
	for _, item := range output.Items() {
		if item.Kind != canonical.ItemKindText {
			return effect.Result[carrier.CarrierDocument]{}, canonical.UnsupportedOperation("completions protocol does not support tool-bearing output items")
		}
	}
	raw, err := json.Marshal(completionsResponseDTO{
		ID:     sse.FallbackID(output.ResultID(), "cmpl_swobu"),
		Object: "text_completion",
		Model:  output.Model(),
		Choices: []completionsChoiceDTO{{
			Index:        0,
			Text:         sse.OutputText(output.Items()),
			FinishReason: sse.DefaultFinishReason(output.FinishReason(), "stop"),
		}},
		Usage: completionsUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return effect.Result[carrier.CarrierDocument]{}, err
	}
	return effect.NewResult(carrier.NewCarrierDocument("", protocolkind.Completions, "application/json", nil, raw, carrier.Meta{})), nil
}
