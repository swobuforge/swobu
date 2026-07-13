package completions

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func EncodeCarrier(req canonical.CanonicalRequest, d delivery.Delivery) (carrier.WireDocument, error) {
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return carrier.WireDocument{}, canonical.UnsupportedDelivery("prompt requests do not implement the requested delivery mode on the completions protocol")
	}
	if !req.ToolPolicy().IsZero() {
		return carrier.WireDocument{}, canonical.UnsupportedOperation("completions protocol does not support tool choice")
	}
	if len(req.Tools()) > 0 {
		return carrier.WireDocument{}, canonical.UnsupportedOperation("completions protocol does not support tool declarations")
	}
	if err := rejectCompletionsOutputFormat(req.OutputFormat()); err != nil {
		return carrier.WireDocument{}, err
	}

	prompt, err := promptFromItems(req.Items())
	if err != nil {
		return carrier.WireDocument{}, err
	}

	payload := map[string]any{
		"model":  req.Model(),
		"prompt": prompt,
	}
	if err := encodeCompletionsGenerationControls(payload, req.Controls()); err != nil {
		return carrier.WireDocument{}, err
	}
	if d.Mode == delivery.Streaming {
		payload["stream"] = true
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return carrier.WireDocument{}, canonical.BadRequest("prompt request could not be encoded for the completions protocol")
	}

	return carrier.NewWireDocument(
		carrier.StageProviderRequestOut,
		"",
		"application/json",
		nil,
		raw,
		carrier.Meta{},
	), nil
}

func promptFromItems(items []canonical.CanonicalItem) (string, error) {
	out := ""
	for _, item := range items {
		switch item.Kind {
		case canonical.ItemKindText:
			out += item.Text
		case canonical.ItemKindToolUse, canonical.ItemKindToolResult:
			return "", canonical.UnsupportedOperation("completions protocol does not support tool-bearing canonical items")
		default:
			return "", canonical.UnsupportedOperation("completions protocol does not support this canonical item kind")
		}
	}
	return out, nil
}
