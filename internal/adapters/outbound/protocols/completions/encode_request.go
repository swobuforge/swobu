package completions

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func EncodeRequest(request canonical.CanonicalRequest, deliveryMode bool) (protocols.WireRequest, error) {
	switch typed := request.(type) {
	case canonical.PromptCanonicalRequest:
		return Encode(typed, deliveryMode)
	default:
		return protocols.WireRequest{}, canonical.UnsupportedOperation("completions protocol does not implement the canonical semantic request")
	}
}

type requestBody struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream,omitempty"`
}

func Encode(req canonical.PromptCanonicalRequest, deliveryMode bool) (protocols.WireRequest, error) {
	switch deliveryMode {
	case false, true:
	default:
		return protocols.WireRequest{}, canonical.UnsupportedDelivery("prompt requests do not implement the requested delivery variant on the completions protocol")
	}

	raw, err := json.Marshal(requestBody{
		Model:  req.Model(),
		Prompt: req.Prompt(),
		Stream: deliveryMode == true,
	})
	if err != nil {
		return protocols.WireRequest{}, canonical.BadRequest("prompt request could not be encoded for the completions protocol")
	}

	return protocols.WireRequest{
		Method:  http.MethodPost,
		Path:    "/completions",
		Body:    bytes.NewReader(raw),
		HasBody: true,
	}, nil
}
