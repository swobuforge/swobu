package completions

import (
	"encoding/json"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
)

func (ResponseDocumentEncoder) EncodeResponseDocument(output canonical.CanonicalOutput) (exchange.Result[carrier.WireDocument], error) {
	for _, item := range output.Items() {
		if item.Kind != canonical.ItemKindText {
			return exchange.Result[carrier.WireDocument]{}, canonical.UnsupportedOperation("completions protocol does not support tool-bearing output items")
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
		return exchange.Result[carrier.WireDocument]{}, err
	}
	return exchange.NewResult(carrier.NewWireDocument("", protocolkind.Completions, "application/json", nil, raw, carrier.Meta{})), nil
}
