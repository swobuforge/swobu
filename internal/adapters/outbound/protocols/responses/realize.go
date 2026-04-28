package responses

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/metrofun/swobu/internal/adapters/outbound/protocols"
	"github.com/metrofun/swobu/internal/domain/compatibility"
)

func Realize(request compatibility.CanonicalRequest, deliveryMode compatibility.DeliveryMode) (protocols.WireRequest, error) {
	switch typed := request.(type) {
	case compatibility.GenerationCanonicalRequest:
		if err := compatibility.ValidateResponseContinuationSelectors(typed); err != nil {
			return protocols.WireRequest{}, err
		}
		return Encode(typed, deliveryMode)
	case compatibility.DialogCanonicalRequest:
		return Encode(compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
			Model:    typed.Model(),
			Thread:   typed.Items(),
			LastTurn: typed.Items(),
		}), deliveryMode)
	default:
		return protocols.WireRequest{}, compatibility.UnsupportedOperation("responses protocol does not implement the canonical semantic request")
	}
}

type requestBody struct {
	Model                string `json:"model"`
	Input                any    `json:"input,omitempty"`
	ToolChoice           any    `json:"tool_choice,omitempty"`
	PreviousResponseID   string `json:"previous_response_id,omitempty"`
	PromptCacheKey       string `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention string `json:"prompt_cache_retention,omitempty"`
	Stream               bool   `json:"stream,omitempty"`
}

type inputMessageItem struct {
	Type    string         `json:"type"`
	Role    string         `json:"role"`
	Content []inputTextRef `json:"content"`
}

type inputTextRef struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type functionCallItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type functionCallOutputItem struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

func Encode(req compatibility.GenerationCanonicalRequest, deliveryMode compatibility.DeliveryMode) (protocols.WireRequest, error) {
	switch deliveryMode {
	case compatibility.DeliveryModeBuffered, compatibility.DeliveryModeStreaming:
	default:
		return protocols.WireRequest{}, compatibility.UnsupportedDelivery("response requests do not implement the requested delivery mode on the responses protocol")
	}

	input, err := encodeInput(req)
	if err != nil {
		return protocols.WireRequest{}, err
	}

	raw, err := json.Marshal(requestBody{
		Model:                req.Model(),
		Input:                input,
		ToolChoice:           encodeToolChoice(req.ToolMode()),
		PreviousResponseID:   req.PreviousResponseID(),
		PromptCacheKey:       req.PromptCacheKey(),
		PromptCacheRetention: req.PromptCacheRetention(),
		Stream:               deliveryMode == compatibility.DeliveryModeStreaming,
	})
	if err != nil {
		return protocols.WireRequest{}, compatibility.BadRequest("response request could not be encoded for the responses protocol")
	}

	return protocols.WireRequest{
		Method:  http.MethodPost,
		Path:    "/responses",
		Body:    bytes.NewReader(raw),
		HasBody: true,
	}, nil
}

func encodeToolChoice(mode compatibility.ToolMode) any {
	switch mode {
	case compatibility.ToolModeAuto:
		return "auto"
	case compatibility.ToolModeRequired:
		return "required"
	default:
		return nil
	}
}

func encodeInput(req compatibility.GenerationCanonicalRequest) (any, error) {
	if input, ok, err := encodeSimpleInput(req); ok || err != nil {
		return input, err
	}
	switch {
	case req.HasLastTurn():
		// When compatibility derived a truthful suffix against a known prefix
		// anchor, responses can use that cheaper incremental view directly.
		return encodeConversation(req.LastTurn())
	case req.HasThread():
		// Full thread remains the authoritative fallback for every cross-protocol
		// path. Responses optimization must never outrank semantic truth.
		return encodeConversation(req.Thread())
	default:
		return nil, nil
	}
}

func encodeSimpleInput(req compatibility.GenerationCanonicalRequest) (any, bool, error) {
	var messages []compatibility.CanonicalItem
	switch {
	case req.HasLastTurn():
		messages = req.LastTurn()
	case req.HasThread():
		messages = req.Thread()
	default:
		return nil, false, nil
	}
	if len(messages) != 1 {
		return nil, false, nil
	}
	if messages[0].Author != "" && messages[0].Author != compatibility.ItemAuthorUser {
		return nil, false, nil
	}
	text, ok := textOnlyItem(messages[0])
	if !ok {
		return nil, false, nil
	}
	return text, true, nil
}

func textOnlyItem(item compatibility.CanonicalItem) (string, bool) {
	if item.Kind != compatibility.ItemKindText {
		return "", false
	}
	return item.Text, true
}

func encodeConversation(items []compatibility.CanonicalItem) ([]any, error) {
	encoded := make([]any, 0, len(items))
	for i := 0; i < len(items); {
		current := items[i]
		switch current.Kind {
		case compatibility.ItemKindText:
			role := roleForResponsesItem(current)
			content := make([]inputTextRef, 0, 1)
			for i < len(items) && items[i].Kind == compatibility.ItemKindText && roleForResponsesItem(items[i]) == role {
				content = append(content, inputTextRef{
					Type: "input_text",
					Text: items[i].Text,
				})
				i++
			}
			encoded = append(encoded, inputMessageItem{
				Type:    "message",
				Role:    role,
				Content: content,
			})
		case compatibility.ItemKindToolUse:
			args, err := json.Marshal(current.Input)
			if err != nil {
				return nil, compatibility.BadRequest("tool_use input could not be encoded for the responses protocol")
			}
			encoded = append(encoded, functionCallItem{
				Type:      "function_call",
				CallID:    current.ToolUseID,
				Name:      current.Name,
				Arguments: string(args),
			})
			i++
		case compatibility.ItemKindToolResult:
			if strings.TrimSpace(current.ToolUseID) == "" {
				return nil, compatibility.BadRequest("tool_result items require tool_use_id for the responses protocol")
			}
			encoded = append(encoded, functionCallOutputItem{
				Type:   "function_call_output",
				CallID: current.ToolUseID,
				Output: current.Text,
			})
			i++
		default:
			return nil, compatibility.UnsupportedOperation("canonical item is not supported on the responses protocol")
		}
	}
	return encoded, nil
}

func roleForResponsesItem(item compatibility.CanonicalItem) string {
	switch item.Author {
	case compatibility.ItemAuthorAssistant:
		return "assistant"
	case compatibility.ItemAuthorTool:
		return "tool"
	default:
		return "user"
	}
}
