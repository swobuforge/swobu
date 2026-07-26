package chatcompletions

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// ProviderRequestMessage is one lowered Chat Completions message. SourceStart
// and SourceEnd retain the canonical association for provider dialect fields
// and are never serialized.
type ProviderRequestMessage struct {
	Role        string         `json:"role"`
	Content     any            `json:"content,omitempty"`
	ToolCalls   []toolCallBody `json:"tool_calls,omitempty"`
	ToolCallID  string         `json:"tool_call_id,omitempty"`
	SourceStart int            `json:"-"`
	SourceEnd   int            `json:"-"`
}

// ProviderRequestDocument is the standard Chat Completions lowering before
// its single serialization boundary.
type ProviderRequestDocument struct {
	Payload             map[string]any
	Messages            []ProviderRequestMessage
	Tools               []ProviderRequestTool
	ToolChoice          any
	MaxTokens           *int
	MaxCompletionTokens *int
	providerTools       any
}

type toolCallBody struct {
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type"`
	Function *toolFunctionBody `json:"function,omitempty"`
	Custom   *toolCustomBody   `json:"custom,omitempty"`
}

type toolFunctionBody struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type toolCustomBody struct {
	Name  string `json:"name"`
	Input string `json:"input"`
}

func EncodeCarrierWithDecisions(req canonical.CanonicalRequest, d delivery.Delivery, sink compat.Sink, exchangeID string) (carrier.Document, error) {
	document, err := LowerProviderRequestDocument(req, d, sink, exchangeID)
	if err != nil {
		return carrier.Document{}, err
	}
	if err := ApplyStandardProviderRequestReasoning(&document, req, sink, exchangeID); err != nil {
		return carrier.Document{}, err
	}
	return EncodeProviderRequestDocument(document)
}

// LowerProviderRequestDocument produces the standard typed Chat Completions
// document before any exact-provider dialect adaptation.
func LowerProviderRequestDocument(req canonical.CanonicalRequest, d delivery.Delivery, sink compat.Sink, exchangeID string) (ProviderRequestDocument, error) {
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return ProviderRequestDocument{}, provider.NewIncompatibleTarget("Chat Completions target cannot represent the requested canonical delivery mode")
	}
	contextRejected, contextErr := projectChatResponsesReasoningContext(req.Reasoning(), sink, exchangeID)
	if contextErr != nil && !contextRejected {
		return ProviderRequestDocument{}, contextErr
	}
	items := req.Items()
	environment, err := canonical.ToolEnvironmentAt(items, len(items))
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	tools := environment.Declarations()
	conversation := make([]canonical.CanonicalItem, 0, len(items))
	historyStarted := false
	for _, item := range items {
		if _, declarations := item.ToolDeclarations(); declarations {
			if historyStarted {
				if err := emitChatImageDecision(sink, exchangeID, compat.RequestTools, compat.Approx); err != nil {
					return ProviderRequestDocument{}, err
				}
			}
			continue
		}
		if message, ok := item.Message(); !ok || message.Role() != canonical.MessageRoleSystem && message.Role() != canonical.MessageRoleDeveloper {
			historyStarted = true
		}
		conversation = append(conversation, item)
	}
	wireMessages, err := encodeItems(conversation, tools, sink, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if contextErr != nil {
		return ProviderRequestDocument{}, contextErr
	}
	wireTools, err := encodeChatCompletionsTools(tools, sink, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if d.Mode == delivery.Streaming && hasChatCompletionsCustomTools(tools) {
		return ProviderRequestDocument{}, provider.NewIncompatibleTarget("Chat Completions streaming cannot represent canonical custom tool declarations")
	}
	policy, err := req.EffectiveToolPolicy()
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	choice, err := encodeChatCompletionsToolChoice(policy, tools, sink, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	logChatCompletionsEncodeShape(req, tools, wireMessages, choice, policy, d)

	payload := map[string]any{
		"model":    req.Model(),
		"messages": wireMessages,
	}
	if hasChatCompletionsWebSearch(tools) {
		payload["web_search_options"] = map[string]any{}
	}
	if err := encodeChatCompletionsToolCallBatch(payload, req.ToolCallBatch(), len(tools) > 0); err != nil {
		return ProviderRequestDocument{}, err
	}
	if err := encodeChatCompletionsGenerationControls(payload, req.Controls()); err != nil {
		return ProviderRequestDocument{}, err
	}
	var maxTokens *int
	if value, ok := req.Controls().Limits.MaxOutputTokens.Value(); ok {
		maxTokens = &value
	}
	delete(payload, "max_tokens")
	payload["messages"] = wireMessages
	document := ProviderRequestDocument{Payload: payload, Messages: wireMessages, Tools: wireTools, ToolChoice: choice, MaxTokens: maxTokens}
	if responseFormat, err := encodeChatCompletionsOutputFormat(req.OutputFormat()); err != nil {
		return ProviderRequestDocument{}, err
	} else if len(responseFormat) > 0 {
		payload["response_format"] = json.RawMessage(responseFormat)
		if req.OutputFormat().Kind == canonical.OutputFormatJSONSchema {
			for _, feature := range []compat.Feature{compat.RequestOutputFormat, compat.RequestOutputFormatSchema, compat.WireJSONMode} {
				if err := emitChatImageDecision(sink, exchangeID, feature, compat.Exact); err != nil {
					return ProviderRequestDocument{}, err
				}
			}
		}
	}
	if d.Mode == delivery.Streaming {
		payload["stream"] = true
	}
	return document, nil
}

// projectChatResponsesReasoningContext makes protocol loss target-local.
// Recording precedes other structural lowering so an independent image/tool
// rejection cannot erase the context evidence.
func projectChatResponsesReasoningContext(reasoning canonical.ReasoningControls, sink compat.Sink, exchangeID string) (bool, error) {
	if !reasoning.ResponsesContextField().IsSpecified() {
		return false, nil
	}
	if err := emitChatImageDecision(sink, exchangeID, compat.RequestReasoningContextResponses, compat.Drop); err != nil {
		return false, err
	}
	return false, nil
}

// ReplaceTools lets an exact provider compose its own typed tool union before
// the protocol's single serialization boundary. Provider syntax remains in the
// provider package; this document only owns where the final tools value lives.
func (d *ProviderRequestDocument) ReplaceTools(tools any) {
	d.providerTools = tools
}

// ApplyStandardProviderRequestReasoning composes the standard Chat
// Completions reasoning spelling into a typed provider document. Exact
// providers with a different reasoning contract intentionally omit it.
func ApplyStandardProviderRequestReasoning(document *ProviderRequestDocument, req canonical.CanonicalRequest, sink compat.Sink, exchangeID string) error {
	if err := encodeChatCompletionsReasoning(document.Payload, req); err != nil {
		if decisionErr := emitChatImageDecision(sink, exchangeID, compat.RequestReasoning, compat.Reject); decisionErr != nil {
			return decisionErr
		}
		return err
	}
	return nil
}

// EncodeProviderRequestDocument performs the single serialization boundary
// after shared lowering or exact-provider adaptation.
func EncodeProviderRequestDocument(document ProviderRequestDocument) (carrier.Document, error) {
	if document.providerTools != nil {
		document.Payload["tools"] = document.providerTools
	} else if len(document.Tools) > 0 {
		document.Payload["tools"] = document.Tools
	} else {
		delete(document.Payload, "tools")
	}
	if document.ToolChoice != nil {
		document.Payload["tool_choice"] = document.ToolChoice
	} else {
		delete(document.Payload, "tool_choice")
	}
	if document.MaxTokens != nil {
		document.Payload["max_tokens"] = *document.MaxTokens
	} else {
		delete(document.Payload, "max_tokens")
	}
	if document.MaxCompletionTokens != nil {
		document.Payload["max_completion_tokens"] = *document.MaxCompletionTokens
	} else {
		delete(document.Payload, "max_completion_tokens")
	}
	raw, err := json.Marshal(document.Payload)
	if err != nil {
		return carrier.Document{}, canonical.BadRequest("conversation request could not be encoded for the chat completions protocol")
	}

	// Stage marks the carrier boundary for this wire leg; exchange path
	// selection happens above this adapter.
	return carrier.NewDocument(
		"",
		"application/json",
		nil,
		raw,
		carrier.Meta{},
	), nil
}

func hasChatCompletionsWebSearch(tools []canonical.ToolDeclaration) bool {
	for _, tool := range tools {
		if tool.Kind() == canonical.ToolKindWebSearch {
			return true
		}
	}
	return false
}

func logChatCompletionsEncodeShape(req canonical.CanonicalRequest, tools []canonical.ToolDeclaration, wireMessages []ProviderRequestMessage, choice any, policy canonical.ToolPolicy, d delivery.Delivery) {
	instructions := chatCompletionsInstructionText(req.Items())
	toolChoiceMode := strings.TrimSpace(string(policy.Mode)) // swobu:io-string source=domain
	toolChoiceSpecific := ""
	if policy.Mode == canonical.ToolPolicySpecific {
		if specific, ok := policy.SpecificID(); ok {
			toolChoiceSpecific = specific.String()
		}
	}
	slog.Debug("chat completions encode",
		"component", "protocol.chat_completions",
		"event", "outbound_request_shape",
		"streaming", d.Mode == delivery.Streaming,
		"instructions_present", instructions != "",
		"instructions_bytes", len(instructions),
		"message_count", len(wireMessages),
		"tool_count", len(tools),
		"function_tool_count", chatCompletionsToolKindCount(tools, canonical.ToolTypeFunction),
		"custom_tool_count", chatCompletionsToolKindCount(tools, canonical.ToolTypeCustom),
		"tool_policy", toolChoiceMode,
		"tool_policy_specific", toolChoiceSpecific,
		"tool_choice_wired", chatCompletionsWireToolChoice(choice),
		"parallel_tool_calls", strings.TrimSpace(string(req.ToolCallBatch().Mode)), // swobu:io-string source=domain
	)
}

func chatCompletionsInstructionText(items []canonical.CanonicalItem) string {
	var out strings.Builder
	for _, item := range items {
		message, ok := item.Message()
		if !ok || message.Role() != canonical.MessageRoleSystem && message.Role() != canonical.MessageRoleDeveloper {
			continue
		}
		for _, part := range message.Content() {
			if value, ok := part.Text(); ok {
				out.WriteString(value.Text())
			}
		}
	}
	return strings.TrimSpace(out.String()) // swobu:io-string source=domain
}

func chatCompletionsToolKindCount(tools []canonical.ToolDeclaration, kind string) int {
	count := 0
	for _, tool := range tools {
		if string(tool.Kind()) == kind {
			count++
		}
	}
	return count
}

func chatCompletionsWireToolChoice(choice any) string {
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

// swobu:lint ignore string-switch because=protocol boundary encodes canonical tool-call kinds.
func encodeItems(items []canonical.CanonicalItem, tools []canonical.ToolDeclaration, sink compat.Sink, exchangeID string) ([]ProviderRequestMessage, error) {
	var err error
	items, err = projectChatResponsesItems(items, sink, exchangeID)
	if err != nil {
		return nil, err
	}
	out := make([]ProviderRequestMessage, 0, len(items))
	var activeBatch *chatActiveToolBatch
	for i := 0; i < len(items); {
		sourceStart := i
		for i < len(items) && items[i].Kind() == canonical.ItemKindReasoning {
			i++
		}
		if i == len(items) {
			break
		}
		item := items[i]
		if item.Kind() == canonical.ItemKindToolResult {
			run, next, updatedBatch, err := encodeChatToolResultRun(items, i, activeBatch, sink, exchangeID)
			if err != nil {
				return nil, err
			}
			out = append(out, run...)
			activeBatch = updatedBatch
			i = next
			continue
		}
		wire := ProviderRequestMessage{SourceStart: sourceStart}
		if message, ok := item.Message(); ok {
			wire.Role = string(message.Role())
			content, err := encodeChatMessageContent(message.Role(), message.Content(), sink, exchangeID)
			if err != nil {
				return nil, err
			}
			wire.Content = content
			i++
		} else if item.Kind() == canonical.ItemKindToolCall {
			wire.Role = "assistant"
		} else {
			return nil, provider.NewIncompatibleTarget("Chat Completions cannot represent this canonical item kind")
		}
		if wire.Role == "assistant" {
			callIDs := make([]canonical.ToolCallID, 0)
			for i < len(items) && items[i].Kind() == canonical.ItemKindToolCall {
				call, _ := items[i].ToolCall()
				encoded, err := encodeChatToolCall(call, tools)
				if err != nil {
					return nil, err
				}
				wire.ToolCalls = append(wire.ToolCalls, encoded)
				callIDs = append(callIDs, call.CallID())
				i++
			}
			if len(callIDs) > 0 {
				if activeBatch != nil && !activeBatch.closed() {
					return nil, provider.NewIncompatibleTarget("Chat Completions cannot start a canonical tool-call batch while a prior batch is unresolved")
				}
				activeBatch = newChatActiveToolBatch(callIDs)
			}
		}
		wire.SourceEnd = i
		out = append(out, wire)
	}
	return out, nil
}

func projectChatResponsesItems(items []canonical.CanonicalItem, sink compat.Sink, exchangeID string) ([]canonical.CanonicalItem, error) {
	projected := make([]canonical.CanonicalItem, 0, len(items))
	for _, item := range items {
		if reasoning, ok := item.Reasoning(); ok {
			if _, present := reasoning.Opaque().Responses(); present {
				if err := emitChatImageDecision(sink, exchangeID, compat.RequestItemsResponsesReasoningReplay, compat.Drop); err != nil {
					return nil, err
				}
			}
		}
		projected = append(projected, item)
	}
	return projected, nil
}

func encodeChatMessageContent(author canonical.MessageRole, parts []canonical.MessagePart, sink compat.Sink, exchangeID string) (any, error) {
	if len(parts) == 1 {
		if text, ok := parts[0].Text(); ok {
			return text.Text(), nil
		}
	}
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		if text, ok := part.Text(); ok {
			out = append(out, map[string]any{"type": "text", "text": text.Text()})
			continue
		}
		if part.Kind() == canonical.PartKindImage {
			if author != canonical.MessageRoleUser {
				return nil, provider.NewIncompatibleTarget("Chat Completions accepts canonical image input only in user messages")
			}
			imagePart, _ := part.Image()
			encoded, err := encodeChatImage(imagePart, compat.RequestItemsMessageImageDetail, sink, exchangeID)
			if err != nil {
				return nil, err
			}
			out = append(out, encoded)
			continue
		}
		return nil, provider.NewIncompatibleTarget("Chat Completions cannot represent this canonical content kind")
	}
	return out, nil
}

func emitChatImageDecision(sink compat.Sink, exchangeID string, feature compat.Feature, outcome compat.Outcome) error {
	if sink == nil {
		return nil
	}
	if err := sink.Commit(context.Background(), exchangeID, []compat.Decision{{
		Feature: feature,
		Outcome: outcome,
		Subject: compat.Subject("canonical:" + string(feature)),
	}}); err != nil {
		return canonical.InternalError("compatibility decision sink commit failed")
	}
	return nil
}

// swobu:lint ignore string-switch because=protocol boundary encodes canonical declaration kinds into chat-completions wire variants.
func encodeChatToolCall(call canonical.ToolCallItem, _ []canonical.ToolDeclaration) (toolCallBody, error) {
	tool := call.Tool()
	name := tool.Name()
	switch tool.Kind() {
	case canonical.ToolKindFunction:
		object, ok := call.Input().Object()
		if !ok {
			return toolCallBody{}, canonical.BadRequest("chat completions function calls require object input")
		}
		return toolCallBody{ID: call.CallID().String(), Type: "function", Function: &toolFunctionBody{Name: name, Arguments: object.String()}}, nil
	case canonical.ToolKindCustom:
		text, ok := call.Input().Text()
		if !ok {
			return toolCallBody{}, canonical.BadRequest("chat completions custom calls require text input")
		}
		return toolCallBody{ID: call.CallID().String(), Type: "custom", Custom: &toolCustomBody{Name: name, Input: text}}, nil
	default:
		return toolCallBody{}, provider.NewIncompatibleTarget("Chat Completions cannot represent this canonical tool-call kind")
	}
}

func textOnlyContent(parts []canonical.MessagePart, surface string) (string, error) {
	var text strings.Builder
	for _, part := range parts {
		value, ok := part.Text()
		if !ok {
			return "", provider.NewIncompatibleTarget(surface + " cannot represent this canonical content kind")
		}
		text.WriteString(value.Text())
	}
	return text.String(), nil
}

func toolResultTextOnlyContent(parts []canonical.ToolResultPart, surface string) (string, error) {
	var text strings.Builder
	for _, part := range parts {
		value, ok := part.Text()
		if !ok {
			return "", provider.NewIncompatibleTarget(surface + " cannot represent this canonical content kind")
		}
		text.WriteString(value.Text())
	}
	return text.String(), nil
}
