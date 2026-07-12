package responses

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
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
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Status  string `json:"status,omitempty"`
	Role    string `json:"role"`
	Content string `json:"content"`
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

	tools := req.Tools()
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
	if choice, err := encodeToolChoice(req.ToolPolicy(), tools); err != nil {
		return carrier.WireDocument{}, err
	} else if choice != nil {
		payload["tool_choice"] = choice
	}
	if wireTools, err := encodeResponsesTools(tools); err != nil {
		return carrier.WireDocument{}, err
	} else if len(wireTools) > 0 {
		payload["tools"] = wireTools
	}
	if err := encodeResponsesToolCallBatch(payload, req.ToolCallBatch(), len(tools) > 0); err != nil {
		return carrier.WireDocument{}, err
	}
	if err := encodeResponsesGenerationControls(payload, req.Controls()); err != nil {
		return carrier.WireDocument{}, err
	}
	if text, err := encodeResponsesOutputFormat(req.OutputFormat()); err != nil {
		return carrier.WireDocument{}, err
	} else if text != nil {
		payload["text"] = text
	}
	if prev, ok := req.Turn().PreviousID(); ok {
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
	encodedItems := thread
	if !req.Turn().IsZero() {
		encodedItems = canonical.CurrentTurnDelta(thread)
	}
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
		"has_previous_response_id", !req.Turn().IsZero(), // swobu:io-string source=boundary
		"thread_item_count", len(thread),
		"encoded_item_count", len(encodedItems),
		"thread_tail_role", responsesTailRole(thread),
		"encoded_tail_role", responsesTailRole(encodedItems),
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

func encodeInput(req canonical.CanonicalRequest, forceStructuredInput bool) (any, error) {
	items := req.Items()
	if !req.Turn().IsZero() {
		items = canonical.CurrentTurnDelta(items)
		// Native continuation-only calls should rely on previous_response_id without
		// replaying anchor thread input. Replaying can end with assistant output and
		// violate backend prefill constraints.
		if !hasContinuationDelta(items) { // swobu:io-string source=boundary
			return nil, nil
		}
	}
	if !forceStructuredInput {
		if input, ok, err := encodeSimpleInput(items); ok || err != nil {
			return input, err
		}
	}
	switch len(items) {
	case 0:
		return nil, nil
	default:
		return encodeConversation(items)
	}
}

func encodeSimpleInput(items []canonical.CanonicalItem) (any, bool, error) {
	if len(items) == 0 {
		return nil, false, nil
	}
	if len(items) != 1 {
		return nil, false, nil
	}
	if items[0].Author != "" && items[0].Author != canonical.ItemAuthorUser {
		return nil, false, nil
	}
	text, ok := textOnlyItem(items[0])
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
		start := i
		current := items[i]
		switch current.Kind {
		case canonical.ItemKindText:
			role := roleForResponsesItem(current)
			var content strings.Builder
			for i < len(items) && items[i].Kind == canonical.ItemKindText && roleForResponsesItem(items[i]) == role {
				content.WriteString(items[i].Text)
				i++
			}
			item := inputMessageItem{
				Type:    "message",
				Role:    role,
				Content: content.String(),
			}
			if role == "assistant" {
				item.ID = sse.FallbackID(current.ItemID, fmt.Sprintf("msg_swobu_%d", start))
				item.Status = "completed"
			}
			encoded = append(encoded, item)
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
