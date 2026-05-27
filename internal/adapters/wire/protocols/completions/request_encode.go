package completions

import (
	"bytes"
	"encoding/json"
	"net/http"

	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func encodeRequest(request canonical.CanonicalRequest, deliveryMode bool) (core.WireRequest, error) {
	items := request.Items()
	prompt := ""
	for _, item := range items {
		if item.Kind == canonical.ItemKindText {
			prompt += item.Text
		}
	}
	return Encode(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:       request.Model(),
		InputText:   prompt,
		CacheIntent: request.CacheIntent(),
	}), deliveryMode)
}

type requestBody struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream,omitempty"`
}

func Encode(req canonical.CanonicalRequest, deliveryMode bool) (core.WireRequest, error) {
	switch deliveryMode {
	case false, true:
	default:
		return core.WireRequest{}, canonical.UnsupportedDelivery("prompt requests do not implement the requested delivery variant on the completions protocol")
	}

	raw, err := json.Marshal(requestBody{
		Model:  req.Model(),
		Prompt: textFromItems(req.Items()),
		Stream: deliveryMode == true,
	})
	if err != nil {
		return core.WireRequest{}, canonical.BadRequest("prompt request could not be encoded for the completions protocol")
	}

	return core.WireRequest{
		Method:  http.MethodPost,
		Path:    "/completions",
		Body:    bytes.NewReader(raw),
		HasBody: true,
	}, nil
}

func textFromItems(items []canonical.CanonicalItem) string {
	out := ""
	for _, item := range items {
		if item.Kind == canonical.ItemKindText {
			out += item.Text
		}
	}
	return out
}
