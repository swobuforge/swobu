package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/responsesnative"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
)

type EncodeOptions struct {
	Instructions  string
	Compatibility compat.CompatibilityPolicy
}

type inputMessageItem struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Status  string `json:"status,omitempty"`
	Role    string `json:"role"`
	Content any    `json:"content"`
	Phase   string `json:"phase,omitempty"`
}

type functionCallItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	ID        string `json:"id,omitempty"`
	Status    string `json:"status,omitempty"`
}

type functionCallOutputItem struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output any    `json:"output"`
}

type customToolCallItem struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Input  string `json:"input"`
	ID     string `json:"id,omitempty"`
	Status string `json:"status,omitempty"`
}

// EncodeInput is the local equivalent of wire.ProviderEncodeInput so this
// package does not import wire.
type EncodeInput struct {
	Request   canonical.CanonicalRequest
	Responses responsesnative.RequestState
}

// ProviderRequestDocument is the standard Responses lowering before an exact
// provider owns any typed dialect adaptation.
type ProviderRequestDocument struct {
	Payload        map[string]any
	Input          any
	InputSpecified bool
	Tools          []ProviderRequestTool
	ToolsSpecified bool
	Store          *bool
}

// swobu:lint ignore function-complexity because=Responses encoding lowers every canonical request band into one atomic wire document.
func EncodeCarrierWithDecisions(input EncodeInput, d delivery.Delivery, sink compat.Sink, exchangeID string, options EncodeOptions) (carrier.Document, error) {
	document, err := LowerProviderRequestDocument(input, d, sink, exchangeID, options)
	if err != nil {
		return carrier.Document{}, err
	}
	return EncodeProviderRequestDocument(document)
}

// LowerProviderRequestDocument produces a typed Responses document without
// crossing the JSON boundary.
func LowerProviderRequestDocument(input EncodeInput, d delivery.Delivery, sink compat.Sink, exchangeID string, options EncodeOptions) (ProviderRequestDocument, error) {
	req := input.Request
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return ProviderRequestDocument{}, canonical.UnsupportedDelivery("response requests do not implement the requested delivery mode on the responses protocol")
	}

	tools := req.Tools()
	previous, hasPrevious := req.PreviousResponse()
	responsesRefined := false
	if hasPrevious {
		if previous.Responses == nil {
			return ProviderRequestDocument{}, fmt.Errorf("responses provider encoding requires a native previous response refinement")
		}
		if err := previous.Responses.ValidateBound(); err != nil {
			return ProviderRequestDocument{}, fmt.Errorf("responses provider encoding received an invalid native previous response refinement: %w", err)
		}
		responsesRefined = true
	}
	var payloadInput any
	var err error
	nativeHistory := input.Responses.History()
	nativeInput := input.Responses.Input()
	if !responsesRefined && (nativeHistory.Len() > 0 || !nativeInput.IsZero()) {
		payloadInput, err = encodeStatelessResponsesInput(nativeHistory, req, nativeInput, options.Compatibility, sink, exchangeID)
	} else if responsesRefined && !nativeInput.IsZero() {
		payloadInput = rawResponsesInputItems(nativeInput)
	} else {
		payloadInput, err = encodeInput(req, options.Compatibility, sink, exchangeID)
	}
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	choice, err := encodeToolChoice(req.EffectiveToolPolicy(), tools, sink, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	logResponsesEncodeShape(req, payloadInput, choice, d)

	payload := map[string]any{"model": req.Model()}
	loweredInstructions := flattenInstructionsForResponses(req.Instructions())
	if err := commitResponsesInstructionDecisions(sink, exchangeID, loweredInstructions); err != nil {
		return ProviderRequestDocument{}, err
	}
	if instructions := mergedResponsesInstructions(loweredInstructions.Text, options.Instructions); instructions != "" || responsesRefined && req.InstructionsSpecified() {
		payload["instructions"] = instructions
	}
	if choice != nil {
		payload["tool_choice"] = choice
	}
	wireTools, err := encodeResponsesTools(tools, sink, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	toolsSpecified := len(wireTools) > 0 || responsesRefined && req.ToolsSpecified()
	if toolsSpecified && wireTools == nil {
		wireTools = []ProviderRequestTool{}
	}
	if err := encodeResponsesToolCallBatch(payload, req.ToolCallBatch(), len(tools) > 0); err != nil {
		return ProviderRequestDocument{}, err
	}
	if responsesRefined && req.ToolCallBatchSpecified() && req.ToolCallBatch().IsZero() && len(tools) > 0 {
		payload["parallel_tool_calls"] = true
	}
	if err := encodeResponsesGenerationControls(payload, req.Controls()); err != nil {
		return ProviderRequestDocument{}, err
	}
	if err := encodeResponsesReasoning(payload, req.Reasoning(), req.Controls().Effort); err != nil {
		if decisionErr := emitResponsesRequestDecision(sink, exchangeID, compat.RequestReasoning, compat.Reject); decisionErr != nil {
			return ProviderRequestDocument{}, decisionErr
		}
		return ProviderRequestDocument{}, err
	}
	// Stateless continuation capture is protocol state, not readable reasoning
	// disclosure. Ask every Responses-compatible target for the encrypted state
	// it may require; runtime rejection remains attempt evidence.
	payload["include"] = []string{"reasoning.encrypted_content"}
	if text, err := encodeResponsesOutputFormat(req.OutputFormat()); err != nil {
		return ProviderRequestDocument{}, err
	} else if text != nil {
		payload["text"] = text
		if req.OutputFormat().Kind == canonical.OutputFormatJSONSchema {
			for _, feature := range []compat.Feature{compat.RequestOutputFormat, compat.RequestOutputFormatSchema, compat.WireJSONMode} {
				if err := emitResponsesRequestDecision(sink, exchangeID, feature, compat.Exact); err != nil {
					return ProviderRequestDocument{}, err
				}
			}
		}
	} else if responsesRefined && req.OutputFormatSpecified() {
		payload["text"] = &responsesTextDTO{Format: responsesTextFormatDTO{Type: string(canonical.OutputFormatText)}}
	}
	if responsesRefined {
		payload["previous_response_id"] = previous.Responses.ProviderResponseID
	}
	if d.Mode == delivery.Streaming {
		payload["stream"] = true
	}
	return ProviderRequestDocument{
		Payload:        payload,
		Input:          payloadInput,
		InputSpecified: payloadInput != nil,
		Tools:          wireTools,
		ToolsSpecified: toolsSpecified,
	}, nil
}

// EncodeProviderRequestDocument performs the single serialization boundary
// after standard lowering or exact-provider typed composition.
func EncodeProviderRequestDocument(document ProviderRequestDocument) (carrier.Document, error) {
	if document.InputSpecified {
		document.Payload["input"] = document.Input
	} else {
		delete(document.Payload, "input")
	}
	if document.ToolsSpecified {
		document.Payload["tools"] = document.Tools
	} else {
		delete(document.Payload, "tools")
	}
	if document.Store != nil {
		document.Payload["store"] = *document.Store
	} else {
		delete(document.Payload, "store")
	}
	raw, err := json.Marshal(document.Payload)
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

func rawResponsesInputItems(input responsesnative.Items) []any {
	items := input.JSONObjects()
	encoded := make([]any, len(items))
	for index, raw := range items {
		encoded[index] = json.RawMessage(raw)
	}
	return encoded
}

func mergedResponsesInstructions(requestInstructions string, optionInstructions string) string {
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
	_, hasPrevious := req.PreviousResponse()
	instructions := strings.TrimSpace(flattenInstructionsForResponses(req.Instructions()).Text) // swobu:io-string source=domain
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
		"has_previous_response_id", hasPrevious, // swobu:io-string source=boundary
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
		"tool_policy", strings.TrimSpace(string(req.EffectiveToolPolicy().Mode)), // swobu:io-string source=domain
		"tool_policy_specific", toolPolicySpecificID(req.EffectiveToolPolicy()),
		"tool_choice_wired", responsesWireToolChoice(choice),
		"parallel_tool_calls", strings.TrimSpace(string(req.ToolCallBatch().Mode)), // swobu:io-string source=domain
	)
}

func responsesTailRole(items []canonical.CanonicalItem) string {
	if len(items) == 0 {
		return ""
	}
	if items[len(items)-1].Kind() == canonical.ItemKindToolResult {
		return "tool"
	}
	if message, ok := items[len(items)-1].Message(); ok {
		return string(message.Role())
	}
	return "assistant"
}

func responsesToolKindCount(tools []canonical.ToolDeclaration, kind string) int {
	count := 0
	for _, tool := range tools {
		if string(tool.Kind()) == kind {
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
			toolType = strings.TrimSpace(toolType) // swobu:io-string source=boundary
			name = strings.TrimSpace(name)         // swobu:io-string source=boundary
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

func encodeInput(req canonical.CanonicalRequest, policy compat.CompatibilityPolicy, sink compat.Sink, exchangeID string) (any, error) {
	items := req.Items()
	if previous, ok := req.PreviousResponse(); ok && previous.Responses != nil && !hasResumptionInput(items) { // swobu:io-string source=boundary
		return nil, nil
	}
	if input, ok, err := encodeSimpleInput(items); ok || err != nil {
		return input, err
	}
	switch len(items) {
	case 0:
		return nil, nil
	default:
		return encodeConversation(req, items, req.Tools(), policy, sink, exchangeID)
	}
}

func encodeStatelessResponsesInput(history responsesnative.History, current canonical.CanonicalRequest, nativeCurrent responsesnative.Items, policy compat.CompatibilityPolicy, sink compat.Sink, exchangeID string) ([]any, error) {
	encoded := make([]any, 0)
	for _, turn := range history.Turns() {
		if native := turn.NativeInput(); !native.IsZero() {
			for _, raw := range native.JSONObjects() {
				encoded = append(encoded, json.RawMessage(raw))
			}
		} else {
			canonicalInput := turn.CanonicalInput()
			inputItems, err := encodeConversation(canonicalInput, canonicalInput.Items(), canonicalInput.Tools(), policy, sink, exchangeID)
			if err != nil {
				return nil, err
			}
			encoded = append(encoded, inputItems...)
		}
		for _, raw := range turn.Output().JSONObjects() {
			encoded = append(encoded, json.RawMessage(raw))
		}
	}
	if !nativeCurrent.IsZero() {
		for _, raw := range nativeCurrent.JSONObjects() {
			encoded = append(encoded, json.RawMessage(raw))
		}
		return encoded, nil
	}
	currentItems, err := encodeConversation(current, current.Items(), current.Tools(), policy, sink, exchangeID)
	if err != nil {
		return nil, err
	}
	return append(encoded, currentItems...), nil
}

func encodeSimpleInput(items []canonical.CanonicalItem) (any, bool, error) {
	if len(items) == 0 {
		return nil, false, nil
	}
	if len(items) != 1 {
		return nil, false, nil
	}
	message, ok := items[0].Message()
	if !ok || message.Role() != canonical.MessageRoleUser {
		return nil, false, nil
	}
	text, ok := textOnlyItem(items[0])
	if !ok {
		return nil, false, nil
	}
	return text, true, nil
}

func textOnlyItem(item canonical.CanonicalItem) (string, bool) {
	message, ok := item.Message()
	if !ok || len(message.Content()) != 1 {
		return "", false
	}
	text, ok := message.Content()[0].Text()
	if !ok {
		return "", false
	}
	return text.Text(), true
}

func hasResumptionInput(items []canonical.CanonicalItem) bool {
	for _, item := range items {
		if item.Kind() == canonical.ItemKindToolResult {
			return true
		}
		if message, ok := item.Message(); ok && message.Role() == canonical.MessageRoleUser {
			return true
		}
	}
	return false
}

// swobu:lint ignore string-switch because=protocol boundary encodes canonical declaration kinds into Responses wire variants.
func encodeConversation(request canonical.CanonicalRequest, items []canonical.CanonicalItem, tools []canonical.ToolDeclaration, policy compat.CompatibilityPolicy, sink compat.Sink, exchangeID string) ([]any, error) {
	encoded := make([]any, 0, len(items))
	for _, current := range items {
		switch current.Kind() {
		case canonical.ItemKindMessage:
			message, _ := current.Message()
			content, err := encodeResponsesMessageContent(message.Role(), message.Content())
			if err != nil {
				return nil, err
			}
			item := inputMessageItem{
				Type:    "message",
				Role:    string(message.Role()),
				Content: content,
			}
			if message.Role() == canonical.MessageRoleAssistant {
				item.Status = "completed"
			}
			encoded = append(encoded, item)
		case canonical.ItemKindToolCall:
			call, _ := current.ToolCall()
			tool := call.Tool()
			name := tool.Name()
			switch tool.Kind() {
			case canonical.ToolKindFunction:
				object, ok := call.Input().Object()
				if !ok {
					return nil, canonical.BadRequest("responses function calls require object input")
				}
				item := functionCallItem{Type: "function_call", CallID: call.CallID().String(), Name: name, Arguments: object.String()}
				encoded = append(encoded, item)
			case canonical.ToolKindCustom:
				text, ok := call.Input().Text()
				if !ok {
					return nil, canonical.BadRequest("responses custom tool calls require text input")
				}
				item := customToolCallItem{Type: "custom_tool_call", CallID: call.CallID().String(), Name: name, Input: text}
				encoded = append(encoded, item)
			default:
				return nil, canonical.UnsupportedOperation("responses protocol does not lower this tool-call kind")
			}
		case canonical.ItemKindToolResult:
			result, _ := current.ToolResult()
			if result.IsError() {
				outcome := compat.Approx
				if policy.EffectiveMode() == compat.CompatibilityStrict {
					outcome = compat.Reject
				}
				if err := emitResponsesRequestDecision(sink, exchangeID, compat.RequestItemsToolResultIsError, outcome); err != nil {
					return nil, err
				}
				if outcome == compat.Reject {
					return nil, canonical.UnsupportedOperation("responses protocol cannot preserve tool-result error state")
				}
			}
			content, err := encodeResponsesToolResultContent(result.Content())
			if err != nil {
				return nil, err
			}
			encoded = append(encoded, functionCallOutputItem{
				Type:   "function_call_output",
				CallID: result.CallID().String(),
				Output: content,
			})
		case canonical.ItemKindReasoning:
			// Manual Responses reasoning replay is outside P0. Native
			// continuation is carried by ResponseRef; portable reasoning remains
			// in checkpoint truth when full history is materialized.
			continue
		default:
			return nil, canonical.UnsupportedOperation("canonical item is not supported on the responses protocol")
		}
	}
	return encoded, nil
}

func emitResponsesRequestDecision(sink compat.Sink, exchangeID string, feature compat.Feature, outcome compat.Outcome) error {
	if sink == nil {
		return nil
	}
	if err := sink.Commit(context.Background(), exchangeID, []compat.Decision{{Feature: feature, Outcome: outcome, Subject: compat.Subject("canonical:" + string(feature))}}); err != nil {
		return canonical.InternalError("compatibility decision sink commit failed")
	}
	return nil
}

func encodeResponsesMessageContent(author canonical.MessageRole, parts []canonical.MessagePart) (any, error) {
	if len(parts) == 1 {
		if text, ok := parts[0].Text(); ok {
			return text.Text(), nil
		}
	}
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		if text, ok := part.Text(); ok {
			out = append(out, map[string]any{"type": "input_text", "text": text.Text()})
			continue
		}
		if part.Kind() == canonical.PartKindImage {
			if author != canonical.MessageRoleUser {
				return nil, canonical.UnsupportedOperation("responses protocol only accepts image input in user messages")
			}
			image, _ := part.Image()
			rawURL, detail, err := openaiwire.EncodeOpenAIImageURL(image)
			if err != nil {
				return nil, canonical.InternalError("canonical image source is invalid")
			}
			wireImage := map[string]any{"type": "input_image", "image_url": rawURL}
			if detail != "" {
				wireImage["detail"] = string(detail)
			}
			out = append(out, wireImage)
			continue
		}
		return nil, canonical.UnsupportedOperation("responses protocol cannot lower this content kind")
	}
	return out, nil
}

func responsesTextOnlyContent(parts []canonical.MessagePart, surface string) (string, error) {
	var builder strings.Builder
	for _, part := range parts {
		text, ok := part.Text()
		if !ok {
			return "", canonical.UnsupportedOperation(surface + " does not support this content kind")
		}
		builder.WriteString(text.Text())
	}
	return builder.String(), nil
}

func encodeResponsesToolResultContent(parts []canonical.ToolResultPart) (any, error) {
	if len(parts) == 1 {
		if text, ok := parts[0].Text(); ok {
			return text.Text(), nil
		}
	}
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		if text, ok := part.Text(); ok {
			out = append(out, map[string]any{"type": "input_text", "text": text.Text()})
			continue
		}
		image, ok := part.Image()
		if !ok {
			return nil, canonical.UnsupportedOperation("responses tool results do not support this content kind")
		}
		rawURL, detail, err := openaiwire.EncodeOpenAIImageURL(image)
		if err != nil {
			return nil, canonical.InternalError("canonical image source is invalid")
		}
		wireImage := map[string]any{"type": "input_image", "image_url": rawURL}
		if detail != "" {
			wireImage["detail"] = string(detail)
		}
		out = append(out, wireImage)
	}
	return out, nil
}
