package responses

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
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

// EncodeInput is the local equivalent of wire.ProviderEncodeInput so this
// package does not import wire. It carries the canonical request plus an
// optional exact-backend provider continuation.
type EncodeInput struct {
	Request            canonical.CanonicalRequest
	NativeContinuation *provider.NativeContinuation
}

// EncodeCarrier is a convenience that encodes a carrier document from a raw
// canonical request without native replay support. It does not infer provider
// continuation from TurnRef; callers that need provider continuation must pass
// the exact-backend NativeContinuation to EncodeCarrierWithDecisions.
func EncodeCarrier(req canonical.CanonicalRequest, d delivery.Delivery) (carrier.Document, error) {
	input := EncodeInput{Request: req}
	return EncodeCarrierWithDecisions(input, d, nil, "", EncodeOptions{})
}

func EncodeCarrierWithDecisions(input EncodeInput, d delivery.Delivery, sink compat.Sink, exchangeID string, options EncodeOptions) (carrier.Document, error) {
	req := input.Request

	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return carrier.Document{}, canonical.UnsupportedDelivery("response requests do not implement the requested delivery mode on the responses protocol")
	}

	tools := req.Tools()
	presence := req.Presence()
	nativeContinuation := input.NativeContinuation != nil
	payloadInput, err := encodeInput(req, options.ForceStructuredInput)
	if err != nil {
		return carrier.Document{}, err
	}
	choice, err := encodeToolChoice(req.ToolPolicy(), tools, sink, exchangeID)
	if err != nil {
		return carrier.Document{}, err
	}
	logResponsesEncodeShape(req, payloadInput, choice, d)

	payload := map[string]any{
		"model": req.Model(),
	}
	if payloadInput != nil {
		payload["input"] = payloadInput
	}
	if instructions := mergedResponsesInstructions(req.Instructions(), options.Instructions); instructions != "" || nativeContinuation && presence.Instructions {
		payload["instructions"] = instructions
	}
	if choice != nil {
		payload["tool_choice"] = choice
	}
	if wireTools, err := encodeResponsesTools(tools, sink, exchangeID); err != nil {
		return carrier.Document{}, err
	} else if len(wireTools) > 0 || nativeContinuation && presence.Tools {
		if wireTools == nil {
			wireTools = []any{}
		}
		payload["tools"] = wireTools
	}
	if err := encodeResponsesToolCallBatch(payload, req.ToolCallBatch(), len(tools) > 0); err != nil {
		return carrier.Document{}, err
	}
	if nativeContinuation && presence.ToolCallBatch && req.ToolCallBatch().IsZero() && len(tools) > 0 {
		payload["parallel_tool_calls"] = true
	}
	if err := encodeResponsesGenerationControls(payload, req.Controls(), presence.Controls, nativeContinuation); err != nil {
		return carrier.Document{}, err
	}
	if text, err := encodeResponsesOutputFormat(req.OutputFormat()); err != nil {
		return carrier.Document{}, err
	} else if text != nil {
		payload["text"] = text
	} else if nativeContinuation && presence.OutputFormat {
		payload["text"] = &responsesTextDTO{Format: responsesTextFormatDTO{Type: string(canonical.OutputFormatText)}}
	}
	if input.NativeContinuation != nil {
		payload["previous_response_id"] = string(input.NativeContinuation.ID)
	}
	if options.Store != nil {
		payload["store"] = *options.Store
	}
	if d.Mode == delivery.Streaming {
		payload["stream"] = true
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return carrier.Document{}, canonical.BadRequest("response request could not be encoded for the responses protocol")
	}

	return carrier.NewDocument(
		"",
		"application/json",
		nil,
		raw,
		carrier.Meta{},
	), nil
}

func mergedResponsesInstructions(requestInstructions string, optionInstructions string) string {
	requestInstructions = strings.TrimSpace(requestInstructions) // swobu:io-string source=boundary
	optionInstructions = strings.TrimSpace(optionInstructions)   // swobu:io-string source=boundary
	switch {
	case requestInstructions == "":
		return optionInstructions
	case optionInstructions == "":
		return requestInstructions
	default:
		return requestInstructions + "\n\n" + optionInstructions
	}
}

func logResponsesEncodeShape(req canonical.CanonicalRequest, input any, choice any, d delivery.Delivery) {
	thread := req.Items()
	encodedItems := thread
	if !req.Turn().IsZero() {
		encodedItems = canonical.CurrentTurnDelta(thread)
	}
	instructions := strings.TrimSpace(req.Instructions()) // swobu:io-string source=domain
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
		"instructions_present", instructions != "",
		"instructions_bytes", len(instructions),
		"thread_item_count", len(thread),
		"encoded_item_count", len(encodedItems),
		"thread_tail_role", responsesTailRole(thread),
		"encoded_tail_role", responsesTailRole(encodedItems),
		"input_type", inputType,
		"tool_count", len(req.Tools()),
		"function_tool_count", responsesToolKindCount(req.Tools(), canonical.ToolTypeFunction),
		"custom_tool_count", responsesToolKindCount(req.Tools(), canonical.ToolTypeCustom),
		"capability_tool_count", responsesCapabilityToolCount(req.Tools()),
		"tool_policy", strings.TrimSpace(string(req.ToolPolicy().Mode)), // swobu:io-string source=domain
		"tool_policy_specific", toolPolicySpecificID(req.ToolPolicy()),
		"tool_choice_wired", responsesWireToolChoice(choice),
		"parallel_tool_calls", strings.TrimSpace(string(req.ToolCallBatch().Mode)), // swobu:io-string source=domain
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

func responsesToolKindCount(tools []canonical.ToolDecl, kind string) int {
	count := 0
	for _, tool := range tools {
		if canonical.ToolDeclKind(tool) == kind {
			count++
		}
	}
	return count
}

func responsesCapabilityToolCount(tools []canonical.ToolDecl) int {
	count := 0
	for _, tool := range tools {
		switch tool.(type) {
		case canonical.CapabilityToolDecl, *canonical.CapabilityToolDecl:
			count++
		}
	}
	return count
}

func toolPolicySpecificID(policy canonical.ToolPolicy) string {
	if policy.Mode != canonical.ToolPolicySpecific {
		return ""
	}
	specific, ok := policy.SpecificID()
	if !ok {
		return ""
	}
	return specific.String()
}

func responsesWireToolChoice(choice any) string {
	switch v := choice.(type) {
	case nil:
		return ""
	case string:
		return v
	case map[string]any:
		if name, ok := v["name"].(string); ok {
			toolType, _ := v["type"].(string)
			toolType = strings.TrimSpace(toolType)
			name = strings.TrimSpace(name)
			if toolType != "" && name != "" {
				return toolType + ":" + name
			}
			if name != "" {
				return name
			}
		}
	}
	return "object"
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
