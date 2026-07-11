package responses

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type EncodeOptions struct {
	Instructions         string
	ForceStructuredInput bool
	Store                *bool
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

func EncodeCarrier(req canonical.CanonicalRequest, d delivery.Delivery) (carrier.WireDocument, error) {
	return encodeCarrierWithOptions(req, d, EncodeOptions{})
}

func encodeCarrierWithOptions(req canonical.CanonicalRequest, d delivery.Delivery, options EncodeOptions) (carrier.WireDocument, error) {
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return carrier.WireDocument{}, canonical.UnsupportedDelivery("response requests do not implement the requested delivery mode on the responses protocol")
	}

	input, err := encodeInput(req, options.ForceStructuredInput)
	if err != nil {
		return carrier.WireDocument{}, err
	}
	logResponsesEncodeShape(req, input, d)

	payload := map[string]any{
		"model": req.Model(),
	}
	if input != nil {
		payload["input"] = input
	}
	if trimmed := strings.TrimSpace(options.Instructions); trimmed != "" { // swobu:io-string source=boundary
		payload["instructions"] = trimmed
	}
	if choice := encodeToolChoice(req.ToolMode()); choice != nil {
		payload["tool_choice"] = choice
	}
	if prev := req.PreviousResponseID(); prev != "" {
		payload["previous_response_id"] = prev
	}
	if options.Store != nil {
		payload["store"] = *options.Store
	}
	if d.Mode == delivery.Streaming {
		payload["stream"] = true
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return carrier.WireDocument{}, canonical.BadRequest("response request could not be encoded for the responses protocol")
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

func logResponsesEncodeShape(req canonical.CanonicalRequest, input any, d delivery.Delivery) {
	thread := req.Items()
	lastTurn := thread
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
		"streaming", d.Mode == delivery.Streaming,
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
	if strings.TrimSpace(req.PreviousResponseID()) != "" && !hasContinuationDelta(req.Items()) { // swobu:io-string source=boundary
		return nil, nil
	}
	if !forceStructuredInput {
		if input, ok, err := encodeSimpleInput(req); ok || err != nil {
			return input, err
		}
	}
	items := req.Items()
	switch len(items) {
	case 0:
		return nil, nil
	default:
		return encodeConversation(items)
	}
}

func encodeSimpleInput(req canonical.CanonicalRequest) (any, bool, error) {
	messages := req.Items()
	if len(messages) == 0 {
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

func hasContinuationDelta(items []canonical.CanonicalItem) bool {
	for _, item := range items {
		if item.Author == canonical.ItemAuthorUser || item.Author == canonical.ItemAuthorTool {
			return true
		}
	}
	return false
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
			encoded = append(encoded, functionCallItem{
				Type:      "function_call",
				CallID:    current.ToolUseID,
				Name:      current.Name,
				Arguments: current.Input.RawObject(),
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
