package codexwire

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols"
	responses "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/responses"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func EncodeRequest(request canonical.CanonicalRequest, _ bool) (protocols.WireRequest, error) {
	// Codex execute path is stream-native; batch clients are handled via
	// stream->batch projection outside this protocol encoder.
	wireReq, err := responses.EncodeRequest(request, true)
	if err != nil {
		return protocols.WireRequest{}, err
	}
	return normalizeCodexRequest(wireReq)
}

func normalizeCodexRequest(wireReq protocols.WireRequest) (protocols.WireRequest, error) {
	if wireReq.Path != "/responses" || !wireReq.HasBody || wireReq.Body == nil {
		return wireReq, nil
	}
	raw, err := io.ReadAll(wireReq.Body)
	if err != nil {
		return protocols.WireRequest{}, canonical.BadRequest("codex request body could not be read")
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return protocols.WireRequest{}, canonical.BadRequest("codex request body could not be decoded")
	}
	if !hasNonEmptyStringField(body, "instructions") {
		body["instructions"] = "You are a helpful assistant."
	}
	if inputText, ok := body["input"].(string); ok && strings.TrimSpace(inputText) != "" { // trimlowerlint:allow boundary canonicalization
		body["input"] = []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "output_text",
						"text": inputText,
					},
				},
			},
		}
	}
	body["store"] = false
	encoded, err := json.Marshal(body)
	if err != nil {
		return protocols.WireRequest{}, canonical.BadRequest("codex request body could not be encoded")
	}
	wireReq.Body = bytes.NewReader(encoded)
	return wireReq, nil
}

func hasNonEmptyStringField(body map[string]any, key string) bool {
	value, ok := body[key]
	if !ok {
		return false
	}
	typed, ok := value.(string)
	if !ok {
		return false
	}
	return strings.TrimSpace(typed) != "" // trimlowerlint:allow boundary canonicalization
}
