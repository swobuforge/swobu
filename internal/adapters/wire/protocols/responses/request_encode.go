package responses

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func encodeRequest(request canonical.CanonicalRequest, deliveryMode bool) (core.WireRequest, error) {
	if err := canonical.ValidateResponseContinuationSelectors(request); err != nil {
		return core.WireRequest{}, err
	}
	return Encode(request, deliveryMode)
}

type EncodeOptions struct {
	Instructions         string
	ForceStructuredInput bool
	Store                *bool
}

func encodeRequestWithOptions(request canonical.CanonicalRequest, deliveryMode bool, options EncodeOptions) (core.WireRequest, error) {
	if err := canonical.ValidateResponseContinuationSelectors(request); err != nil {
		return core.WireRequest{}, err
	}
	return encodeWithOptions(request, deliveryMode, options)
}

type requestBody struct {
	Model              string `json:"model"`
	Input              any    `json:"input,omitempty"`
	Instructions       string `json:"instructions,omitempty"`
	ToolChoice         any    `json:"tool_choice,omitempty"`
	PreviousResponseID string `json:"previous_response_id,omitempty"`
	Store              *bool  `json:"store,omitempty"`
	Stream             bool   `json:"stream,omitempty"`
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

func Encode(req canonical.CanonicalRequest, deliveryMode bool) (core.WireRequest, error) {
	return encodeWithOptions(req, deliveryMode, EncodeOptions{})
}

func encodeWithOptions(req canonical.CanonicalRequest, deliveryMode bool, options EncodeOptions) (core.WireRequest, error) {
	switch deliveryMode {
	case false, true:
	default:
		return core.WireRequest{}, canonical.UnsupportedDelivery("response requests do not implement the requested delivery variant on the responses protocol")
	}

	input, err := encodeInput(req, options.ForceStructuredInput)
	if err != nil {
		return core.WireRequest{}, err
	}
	logResponsesEncodeShape(req, input, deliveryMode)

	raw, err := json.Marshal(requestBody{
		Model:              req.Model(),
		Input:              input,
		Instructions:       strings.TrimSpace(options.Instructions), // swobu:io-string source=boundary
		ToolChoice:         encodeToolChoice(req.ToolMode()),
		PreviousResponseID: req.PreviousResponseID(),
		Store:              options.Store,
		Stream:             deliveryMode == true,
	})
	if err != nil {
		return core.WireRequest{}, canonical.BadRequest("response request could not be encoded for the responses protocol")
	}

	return core.WireRequest{
		Method:  http.MethodPost,
		Path:    "/responses",
		Body:    bytes.NewReader(raw),
		HasBody: true,
	}, nil
}

func logResponsesEncodeShape(req canonical.CanonicalRequest, input any, deliveryMode bool) {
	thread := req.Thread()
	lastTurn := req.LastTurn()
	inputType := "nil"
	if input != nil {
		switch input.(type) {
		case string:
			inputType = "string"
		case []any:
			inputType = "array"
		default:
			inputType = "other"
		}
	}
	slog.Debug("responses encode",
		"component", "protocol.responses",
		"event", "outbound_request_shape",
		"streaming", deliveryMode,
		"has_previous_response_id", strings.TrimSpace(req.PreviousResponseID()) != "", // swobu:io-string source=boundary
		"thread_item_count", len(thread),
		"last_turn_item_count", len(lastTurn),
		"thread_tail_role", responsesTailRole(thread),
		"last_turn_tail_role", responsesTailRole(lastTurn),
		"input_type", inputType,
	)
}

func responsesTailRole(items []canonical.CanonicalItem) string {
	if len(items) == 0 {
		return ""
	}
	tailAuthor := items[len(items)-1].Author
	if tailAuthor == canonical.ItemAuthorAssistant {
		return "assistant"
	}
	if tailAuthor == canonical.ItemAuthorTool {
		return "tool"
	}
	return "user"
}

func encodeToolChoice(mode canonical.ToolMode) any {
	switch mode {
	case canonical.ToolModeAuto:
		return "auto"
	case canonical.ToolModeRequired:
		return "required"
	default:
		return nil
	}
}

func encodeInput(req canonical.CanonicalRequest, forceStructuredInput bool) (any, error) {
	// Native continuation-only calls should rely on previous_response_id without
	// replaying anchor thread input. Replaying can end with assistant output and
	// violate backend prefill constraints.
	if strings.TrimSpace(req.PreviousResponseID()) != "" && !req.HasLastTurn() { // swobu:io-string source=boundary
		return nil, nil
	}
	if !forceStructuredInput {
		if input, ok, err := encodeSimpleInput(req); ok || err != nil {
			return input, err
		}
	}
	switch {
	case req.HasLastTurn():
		// When continuation derivation produced a truthful suffix against a known prefix
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

func encodeSimpleInput(req canonical.CanonicalRequest) (any, bool, error) {
	var messages []canonical.CanonicalItem
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
	if messages[0].Author != "" && messages[0].Author != canonical.ItemAuthorUser {
		return nil, false, nil
	}
	text, ok := textOnlyItem(messages[0])
	if !ok {
		return nil, false, nil
	}
	return text, true, nil
}

func textOnlyItem(item canonical.CanonicalItem) (string, bool) {
	if item.Kind != canonical.ItemKindText {
		return "", false
	}
	return item.Text, true
}

func encodeConversation(items []canonical.CanonicalItem) ([]any, error) {
	encoded := make([]any, 0, len(items))
	for i := 0; i < len(items); {
		current := items[i]
		switch current.Kind {
		case canonical.ItemKindText:
			role := roleForResponsesItem(current)
			content := make([]inputTextRef, 0, 1)
			for i < len(items) && items[i].Kind == canonical.ItemKindText && roleForResponsesItem(items[i]) == role {
				content = append(content, inputTextRef{
					Type: contentPartTypeForRole(role),
					Text: items[i].Text,
				})
				i++
			}
			encoded = append(encoded, inputMessageItem{
				Type:    "message",
				Role:    role,
				Content: content,
			})
		case canonical.ItemKindToolUse:
			args, err := json.Marshal(current.Input)
			if err != nil {
				return nil, canonical.BadRequest("tool_use input could not be encoded for the responses protocol")
			}
			encoded = append(encoded, functionCallItem{
				Type:      "function_call",
				CallID:    current.ToolUseID,
				Name:      current.Name,
				Arguments: string(args),
			})
			i++
		case canonical.ItemKindToolResult:
			if strings.TrimSpace(current.ToolUseID) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("tool_result items require tool_use_id for the responses protocol")
			}
			encoded = append(encoded, functionCallOutputItem{
				Type:   "function_call_output",
				CallID: current.ToolUseID,
				Output: current.Text,
			})
			i++
		default:
			return nil, canonical.UnsupportedOperation("canonical item is not supported on the responses protocol")
		}
	}
	return encoded, nil
}

func contentPartTypeForRole(role string) string {
	if role == "assistant" {
		return "output_text"
	}
	return "input_text"
}

func roleForResponsesItem(item canonical.CanonicalItem) string {
	author := item.Author
	if author == canonical.ItemAuthorAssistant {
		return "assistant"
	}
	if author == canonical.ItemAuthorTool {
		return "tool"
	}
	return "user"
}
