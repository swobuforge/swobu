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
	"github.com/swobuforge/swobu/internal/provider"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
)

type EncodeOptions struct {
	Instructions string
}

type inputMessageItem struct {
	Type    string `json:"type"`
	Status  string `json:"status,omitempty"`
	Role    string `json:"role"`
	Content any    `json:"content"`
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
	Output any    `json:"output"`
}

type customToolCallItem struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Input  string `json:"input"`
}

// EncodeInput is the local equivalent of wire.ProviderEncodeInput so this
// package does not import wire.
type EncodeInput struct {
	Request canonical.CanonicalRequest
}

// ProviderRequestDocument is the standard Responses lowering before an exact
// provider owns any typed dialect adaptation.
type ProviderRequestDocument struct {
	Payload        map[string]any
	Input          any
	InputSpecified bool
	Tools          []ProviderRequestTool
	ToolsSpecified bool
	ToolChoice     any
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
		return ProviderRequestDocument{}, provider.NewIncompatibleTarget("Responses target cannot represent the requested canonical delivery mode")
	}

	items := req.Items()
	fullEnvironment, err := canonical.ToolEnvironmentAt(items, len(items))
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	tools := fullEnvironment.Declarations()
	prelude, _, err := canonical.SplitRequestPrelude(items)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	requestItems := prelude.Items()
	requestEnvironment, err := canonical.ToolEnvironmentAt(requestItems, len(requestItems))
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	requestTools := requestEnvironment.Declarations()
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
	payloadInput, err := encodeInput(req, sink, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	policy, err := req.EffectiveToolPolicy()
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	choice, err := encodeToolChoice(policy, tools, sink, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}

	payload := map[string]any{"model": req.Model()}
	preludeItems := prelude.Items()
	loweredInstructions := flattenInstructionsForResponses(preludeItems)
	logResponsesEncodeShape(req, tools, loweredInstructions.Text, payloadInput, choice, policy, d)
	if err := commitResponsesInstructionDecisions(sink, exchangeID, loweredInstructions); err != nil {
		return ProviderRequestDocument{}, err
	}
	if instructions := mergedResponsesInstructions(loweredInstructions.Text, options.Instructions); instructions != "" {
		payload["instructions"] = instructions
	}
	wireTools, err := encodeResponsesTools(requestTools, sink, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	toolsSpecified := false
	for _, item := range preludeItems {
		if _, ok := item.ToolDeclarations(); ok {
			toolsSpecified = true
			break
		}
	}
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
	if req.Reasoning().ResponsesContextField().IsSpecified() {
		if err := emitResponsesRequestDecision(sink, exchangeID, compat.RequestReasoningContextResponses, compat.Exact); err != nil {
			return ProviderRequestDocument{}, err
		}
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
		ToolChoice:     choice,
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
	if document.ToolChoice != nil {
		document.Payload["tool_choice"] = document.ToolChoice
	} else {
		delete(document.Payload, "tool_choice")
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

func logResponsesEncodeShape(req canonical.CanonicalRequest, tools []canonical.ToolDeclaration, instructions string, input any, choice any, policy canonical.ToolPolicy, d delivery.Delivery) {
	thread := req.Items()
	encodedItems := thread
	_, hasPrevious := req.PreviousResponse()
	instructions = strings.TrimSpace(instructions) // swobu:io-string source=domain
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
		"tool_count", len(tools),
		"function_tool_count", responsesToolKindCount(tools, canonical.ToolTypeFunction),
		"custom_tool_count", responsesToolKindCount(tools, canonical.ToolTypeCustom),
		"tool_policy", strings.TrimSpace(string(policy.Mode)), // swobu:io-string source=domain
		"tool_policy_specific", toolPolicySpecificID(policy),
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

func encodeInput(req canonical.CanonicalRequest, sink compat.Sink, exchangeID string) (any, error) {
	items := req.Items()
	if previous, ok := req.PreviousResponse(); ok && previous.Responses != nil && !hasResumptionInput(items) { // swobu:io-string source=boundary
		return nil, nil
	}
	_, history, err := canonical.SplitRequestPrelude(items)
	if err != nil {
		return nil, err
	}
	if input, ok, err := encodeSimpleInput(history); ok || err != nil {
		return input, err
	}
	switch len(items) {
	case 0:
		return nil, nil
	default:
		environment, err := canonical.EffectiveTools(req)
		if err != nil {
			return nil, err
		}
		return encodeConversation(req, items, environment.Declarations(), sink, exchangeID)
	}
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
func encodeConversation(request canonical.CanonicalRequest, items []canonical.CanonicalItem, tools []canonical.ToolDeclaration, sink compat.Sink, exchangeID string) ([]any, error) {
	encoded := make([]any, 0, len(items))
	pendingWebSearch := make(map[canonical.ToolCallID]int)
	for _, current := range items {
		switch current.Kind() {
		case canonical.ItemKindMessage:
			message, _ := current.Message()
			if message.Scope() == canonical.ContextScopeRequest {
				continue
			}
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
		case canonical.ItemKindToolDeclarations:
			declarations, _ := current.ToolDeclarations()
			if declarations.Scope() == canonical.ContextScopeRequest {
				continue
			}
			wireTools, err := encodeResponsesTools(declarations.Tools().Declarations(), sink, exchangeID)
			if err != nil {
				return nil, err
			}
			encoded = append(encoded, map[string]any{"type": "additional_tools", "role": "developer", "tools": wireTools})
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
			case canonical.ToolKindWebSearch:
				if _, exists := pendingWebSearch[call.CallID()]; exists {
					return nil, canonical.BadRequest("responses web-search history contains a duplicate unresolved call")
				}
				search, ok := call.Input().WebSearch()
				if !ok {
					return nil, canonical.BadRequest("responses web-search calls require typed input")
				}
				action, err := encodeResponsesWebSearchAction(search)
				if err != nil {
					return nil, canonical.BadRequest("responses web-search action could not be encoded")
				}
				pendingWebSearch[call.CallID()] = len(encoded)
				encoded = append(encoded, responsesWireOutputItemDTO{
					ID:     call.CallID().String(),
					Type:   "web_search_call",
					Status: "in_progress",
					Action: action,
				})
			case canonical.ToolKindDiscovery:
				object, ok := call.Input().Object()
				if !ok {
					return nil, canonical.BadRequest("responses tool discovery calls require object input")
				}
				executor, ok := call.DiscoveryExecutor()
				if !ok {
					return nil, canonical.InternalError("responses tool discovery call lost execution ownership")
				}
				execution := "client"
				if executor == canonical.DiscoveryExecutorProvider {
					execution = "server"
				}
				encoded = append(encoded, map[string]any{"type": "tool_search_call", "call_id": call.CallID().String(), "execution": execution, "arguments": json.RawMessage(object.String())})
			default:
				return nil, provider.NewIncompatibleTarget("Responses cannot represent this canonical tool-call kind")
			}
		case canonical.ItemKindToolResult:
			result, _ := current.ToolResult()
			if search, ok := result.WebSearch(); ok {
				index, found := pendingWebSearch[result.CallID()]
				if !found {
					return nil, canonical.BadRequest("responses web-search result has no prior call")
				}
				item, ok := encoded[index].(responsesWireOutputItemDTO)
				if !ok {
					return nil, canonical.InternalError("responses web-search request fold is invalid")
				}
				action, err := encodeResponsesWebSearchSources(search, item.Action)
				if err != nil {
					return nil, canonical.BadRequest("responses web-search sources could not be encoded")
				}
				item.Action = action
				item.Status = "completed"
				encoded[index] = item
				delete(pendingWebSearch, result.CallID())
				continue
			}
			if result.IsError() {
				if err := emitResponsesRequestDecision(sink, exchangeID, compat.RequestItemsToolResultIsError, compat.Approx); err != nil {
					return nil, err
				}
			}
			content, err := encodeResponsesToolResultContent(result.Content())
			if err != nil {
				return nil, err
			}
			item := functionCallOutputItem{
				Type:   "function_call_output",
				CallID: result.CallID().String(),
				Output: content,
			}
			encoded = append(encoded, item)
		case canonical.ItemKindToolDiscoveryResult:
			result, _ := current.ToolDiscoveryResult()
			wireTools, err := encodeResponsesTools(result.Tools().Declarations(), sink, exchangeID)
			if err != nil {
				return nil, err
			}
			execution := "client"
			if result.Executor() == canonical.DiscoveryExecutorProvider {
				execution = "server"
			}
			encoded = append(encoded, map[string]any{"type": "tool_search_output", "call_id": result.CallID().String(), "status": "completed", "execution": execution, "tools": wireTools})
		case canonical.ItemKindReasoning:
			reasoning, _ := current.Reasoning()
			item := map[string]any{"type": "reasoning"}
			if replay, ok := reasoning.Opaque().Responses(); ok {
				item["encrypted_content"] = replay.EncryptedContent
			}
			summary := make([]map[string]any, 0)
			content := make([]map[string]any, 0)
			for _, part := range reasoning.Parts() {
				if part.Kind() == canonical.ReasoningPartSummary {
					summary = append(summary, map[string]any{"type": "summary_text", "text": part.Text()})
				} else {
					content = append(content, map[string]any{"type": "reasoning_text", "text": part.Text()})
				}
			}
			if len(summary) > 0 {
				item["summary"] = summary
			}
			if len(content) > 0 {
				item["content"] = content
			}
			encoded = append(encoded, item)
		default:
			return nil, provider.NewIncompatibleTarget("Responses cannot represent this canonical item kind")
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
				return nil, provider.NewIncompatibleTarget("Responses accepts canonical image input only in user messages")
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
		return nil, provider.NewIncompatibleTarget("Responses cannot represent this canonical content kind")
	}
	return out, nil
}

func responsesTextOnlyContent(parts []canonical.MessagePart, surface string) (string, error) {
	var builder strings.Builder
	for _, part := range parts {
		text, ok := part.Text()
		if !ok {
			return "", provider.NewIncompatibleTarget(surface + " cannot represent this canonical content kind")
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
			return nil, provider.NewIncompatibleTarget("Responses tool results cannot represent this canonical content kind")
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
