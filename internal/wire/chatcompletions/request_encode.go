package chatcompletions

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
)

var reservedMessageFields = map[string]struct{}{
	"role":          {},
	"content":       {},
	"tool_calls":    {},
	"tool_call_id":  {},
	"name":          {},
	"function_call": {},
	"refusal":       {},
	"audio":         {},
}

// ProviderRequestMessage is one lowered Chat Completions message. SourceStart
// and SourceEnd retain the canonical association for provider dialect fields
// and are never serialized.
type ProviderRequestMessage struct {
	Role        string         `json:"role"`
	Content     any            `json:"content,omitempty"`
	ToolCalls   []toolCallBody `json:"tool_calls,omitempty"`
	ToolCallID  string         `json:"tool_call_id,omitempty"`
	Extra       map[string]any `json:"-"`
	SourceStart int            `json:"-"`
	SourceEnd   int            `json:"-"`
}

func (m *ProviderRequestMessage) SetExtra(key string, val any) error {
	trimmed := strings.ToLower(strings.TrimSpace(key))
	if _, reserved := reservedMessageFields[trimmed]; reserved {
		return fmt.Errorf("message extra field %q collides with standard message semantic", key)
	}
	if m.Extra == nil {
		m.Extra = make(map[string]any)
	}
	m.Extra[key] = val
	return nil
}

func (m ProviderRequestMessage) MarshalJSON() ([]byte, error) {
	type wireAlias ProviderRequestMessage
	if len(m.Extra) == 0 {
		return json.Marshal(wireAlias(m))
	}
	base, err := json.Marshal(wireAlias(m))
	if err != nil {
		return nil, err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, err
	}
	for k, v := range m.Extra {
		trimmed := strings.ToLower(strings.TrimSpace(k))
		if _, reserved := reservedMessageFields[trimmed]; reserved {
			return nil, fmt.Errorf("message extra field %q collides with standard message semantic", k)
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		merged[k] = raw
	}
	return json.Marshal(merged)
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

// ToolLoweringContext identifies one declaration at its canonical occurrence.
// A target rule can replace that occurrence but cannot inspect or repair the
// completed output slice.
type ToolLoweringContext struct {
	Ordinal uint32
	Names   wire.ToolNames
}

// ToolLoweringRule returns target fragments for one canonical declaration.
// handled=false delegates to the standard Chat Completions lowering.
type ToolLoweringRule func(ToolLoweringContext, canonical.ToolDeclaration) (fragments []any, handled bool, changes []compat.Change, err error)

// ToolPolicyLoweringRule compiles policy after the effective target tool set
// is known. handled=false delegates to the standard protocol rule.
type ToolPolicyLoweringRule func(canonical.ToolPolicy, wire.LoweredToolSet, wire.ToolNames) (choice any, handled bool, changes []compat.Change, err error)

// MessageLoweringRule mutates or annotates one lowered Chat Completions message
// at its canonical occurrence during traversal.
type MessageLoweringRule func(msg *ProviderRequestMessage, items []canonical.CanonicalItem) error

// ReasoningLoweringRule compiles provider-specific reasoning request fields.
type ReasoningLoweringRule func(req canonical.CanonicalRequest, target ReasoningTargetDialect, changeLog *[]compat.Change, exchangeID string) (map[string]any, error)

// ReasoningTargetDialect exposes only empirical branches an ordinal reasoning
// lowerer can execute. Nil callbacks retain preferred official wire.
type ReasoningTargetDialect struct {
	AcceptsEffortMax func() bool
	AcceptsDisabled  func() bool
}

func (d ReasoningTargetDialect) ProjectEffort(effort canonical.InferenceEffort, changeLog *[]compat.Change) canonical.InferenceEffort {
	if effort != canonical.InferenceEffortMax || d.AcceptsEffortMax == nil || d.AcceptsEffortMax() {
		return effort
	}
	if changeLog != nil {
		*changeLog = compat.AppendUnique(*changeLog, compat.NewApproximation(canonical.RequestControlsEffort, canonical.Occurrence{}))
	}
	return canonical.InferenceEffortXHigh
}

func (d ReasoningTargetDialect) ProjectDisabled(changeLog *[]compat.Change) bool {
	if d.AcceptsDisabled == nil || d.AcceptsDisabled() {
		return true
	}
	if changeLog != nil {
		*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestReasoning, canonical.Occurrence{}))
	}
	return false
}

// CompileOptions contains only proven target-dialect extension points.
type CompileOptions struct {
	LowerTool                  ToolLoweringRule
	LowerToolPolicy            ToolPolicyLoweringRule
	LowerReasoning             ReasoningLoweringRule
	LowerMessage               MessageLoweringRule
	UseMaxCompletionTokens     bool
	AcceptsMaxCompletionTokens func() bool
	OmitParallelToolCallsFalse func() bool
	ReasoningTarget            ReasoningTargetDialect
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

func EncodeCarrierWithChanges(req canonical.CanonicalRequest, names wire.ToolNames, d delivery.Delivery, changeLog *[]compat.Change, exchangeID string) (carrier.Document, error) {
	document, err := CompileProviderRequestDocument(req, names, d, changeLog, exchangeID, CompileOptions{})
	if err != nil {
		return carrier.Document{}, err
	}
	return EncodeProviderRequestDocument(document)
}

// CompileProviderRequestDocument lowers canonical semantics for one exact
// target, preserving source order and resolving dependent policy afterward.
func CompileProviderRequestDocument(req canonical.CanonicalRequest, names wire.ToolNames, d delivery.Delivery, changeLog *[]compat.Change, exchangeID string, options CompileOptions) (ProviderRequestDocument, error) {
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return ProviderRequestDocument{}, canonical.InternalError("Chat Completions received an invalid delivery mode")
	}
	contextRejected, contextErr := projectChatResponsesReasoningContext(req.Reasoning(), changeLog, exchangeID)
	if contextErr != nil && !contextRejected {
		return ProviderRequestDocument{}, contextErr
	}
	items := req.Items()
	items, historyChanges, err := projectChatCompletionsRequestHistory(items)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if changeLog != nil {
		*changeLog = append(*changeLog, historyChanges...)
	}
	if wire.HasDeferredTools(items) {
		if err := appendChatRequestChange(changeLog, exchangeID, canonical.RequestToolsVisibility, compat.Approximation); err != nil {
			return ProviderRequestDocument{}, err
		}
	}
	environment, err := canonical.ToolEnvironmentAt(items, len(items))
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	tools := environment.Declarations()
	if hasProviderDiscovery(tools) {
		tools = removeProviderDiscovery(tools)
		if err := appendChatRequestChange(changeLog, exchangeID, canonical.RequestToolsVisibility, compat.Approximation); err != nil {
			return ProviderRequestDocument{}, err
		}
	}
	flatTools, err := wire.PrepareFlatToolSet(tools, func(tool canonical.ToolDeclaration) (string, error) {
		if tool.Kind() == canonical.ToolKindDiscovery {
			return string(tool.Kind()) + "\x00" + tool.Key().Name(), nil
		}
		name, err := wire.EncodeToolName(names, tool.Key())
		return string(tool.Kind()) + "\x00" + strings.TrimSpace(name), err
	})
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if flatTools.RemovedNamespaces > 0 {
		if err := appendChatRequestChange(changeLog, exchangeID, canonical.RequestTools, compat.Approximation); err != nil {
			return ProviderRequestDocument{}, err
		}
	}
	tools = flatTools.Declarations
	conversation := make([]canonical.CanonicalItem, 0, len(items))
	historyStarted := false
	for _, item := range items {
		if _, declarations := item.ToolDeclarations(); declarations {
			if historyStarted {
				if err := appendChatRequestChange(changeLog, exchangeID, canonical.RequestTools, compat.Approximation); err != nil {
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
	wireMessages, err := encodeItems(conversation, tools, names, changeLog, exchangeID, options.LowerMessage)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if contextErr != nil {
		return ProviderRequestDocument{}, contextErr
	}
	policy, err := req.EffectiveToolPolicy()
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	wireTools, compiledTools, loweredTools, err := compileChatCompletionsTools(tools, names, changeLog, exchangeID, options.LowerTool)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	var choice any
	if options.LowerToolPolicy != nil {
		var handled bool
		var policyChanges []compat.Change
		choice, handled, policyChanges, err = options.LowerToolPolicy(policy, loweredTools, names)
		if changeLog != nil {
			*changeLog = append(*changeLog, policyChanges...)
		}
		if err != nil {
			return ProviderRequestDocument{}, err
		}
		if !handled {
			choice, err = encodeChatCompletionsToolChoice(policy, loweredTools, names, changeLog, exchangeID)
		}
	} else {
		choice, err = encodeChatCompletionsToolChoice(policy, loweredTools, names, changeLog, exchangeID)
	}
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	logChatCompletionsEncodeShape(req, tools, wireMessages, choice, policy, d)

	payload := map[string]any{
		"model":    req.Model(),
		"messages": wireMessages,
	}
	if err := encodeChatCompletionsToolCallBatch(payload, req.ToolCallBatch(), loweredTools.TotalFragments() > 0, options.OmitParallelToolCallsFalse, changeLog); err != nil {
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
	useMaxCompletionTokens := options.UseMaxCompletionTokens && maxTokens != nil &&
		(options.AcceptsMaxCompletionTokens == nil || options.AcceptsMaxCompletionTokens())
	if useMaxCompletionTokens {
		payload["max_completion_tokens"] = *maxTokens
	} else if maxTokens != nil {
		payload["max_tokens"] = *maxTokens
	}
	var providerTools any
	if loweredTools.TotalFragments() > 0 {
		providerTools = compiledTools
	}
	document := ProviderRequestDocument{
		Payload:       payload,
		Messages:      wireMessages,
		Tools:         wireTools,
		ToolChoice:    choice,
		MaxTokens:     maxTokens,
		providerTools: providerTools,
	}
	if useMaxCompletionTokens {
		document.MaxCompletionTokens = maxTokens
		document.MaxTokens = nil
	}
	if responseFormat, err := encodeChatCompletionsOutputFormat(req.OutputFormat()); err != nil {
		return ProviderRequestDocument{}, err
	} else if len(responseFormat) > 0 {
		payload["response_format"] = json.RawMessage(responseFormat)
	}
	if _, err := projectChatResponsesReasoningContext(req.Reasoning(), changeLog, exchangeID); err != nil {
		return ProviderRequestDocument{}, err
	}
	if options.LowerReasoning != nil {
		fields, err := options.LowerReasoning(req, options.ReasoningTarget, changeLog, exchangeID)
		if err != nil {
			return ProviderRequestDocument{}, err
		}
		if err := applyReasoningFields(payload, fields); err != nil {
			return ProviderRequestDocument{}, err
		}
	} else {
		if err := encodeChatCompletionsReasoning(payload, req, options.ReasoningTarget, changeLog); err != nil {
			return ProviderRequestDocument{}, err
		}
	}
	if d.Mode == delivery.Streaming {
		payload["stream"] = true
	}
	return document, nil
}

func hasProviderDiscovery(tools []canonical.ToolDeclaration) bool {
	for _, tool := range tools {
		if discovery, ok := tool.Discovery(); ok && discovery.Executor() == canonical.DiscoveryExecutorProvider {
			return true
		}
	}
	return false
}

func removeProviderDiscovery(tools []canonical.ToolDeclaration) []canonical.ToolDeclaration {
	projected := make([]canonical.ToolDeclaration, 0, len(tools))
	for _, tool := range tools {
		if discovery, ok := tool.Discovery(); ok && discovery.Executor() == canonical.DiscoveryExecutorProvider {
			continue
		}
		projected = append(projected, tool)
	}
	return projected
}

// projectChatResponsesReasoningContext makes protocol loss target-local.
// Recording precedes other structural lowering so an independent image/tool
// rejection cannot erase the context evidence.
func projectChatResponsesReasoningContext(reasoning canonical.ReasoningControls, changeLog *[]compat.Change, exchangeID string) (bool, error) {
	if !reasoning.ResponsesContextField().IsSpecified() {
		return false, nil
	}
	if err := appendChatRequestChange(changeLog, exchangeID, canonical.RequestReasoningContextResponses, compat.Omission); err != nil {
		return false, err
	}
	return false, nil
}

var chatReasoningSemanticFields = map[string]struct{}{
	"reasoning_effort":  {},
	"reasoning":         {},
	"thinking":          {},
	"include_reasoning": {},
}

var nonReasoningChatSemanticFields = map[string]struct{}{
	"model": {}, "messages": {}, "tools": {}, "tool_choice": {}, "parallel_tool_calls": {},
	"functions": {}, "function_call": {}, "stream": {}, "stream_options": {},
	"temperature": {}, "top_p": {}, "max_tokens": {}, "max_completion_tokens": {},
	"stop": {}, "response_format": {}, "n": {}, "presence_penalty": {},
	"frequency_penalty": {}, "seed": {}, "user": {}, "logprobs": {}, "top_logprobs": {},
	"logit_bias": {}, "modalities": {},
}

var reservedChatSemanticFields = func() map[string]struct{} {
	all := make(map[string]struct{}, len(chatReasoningSemanticFields)+len(nonReasoningChatSemanticFields))
	for k := range chatReasoningSemanticFields {
		all[k] = struct{}{}
	}
	for k := range nonReasoningChatSemanticFields {
		all[k] = struct{}{}
	}
	return all
}()

// applyReasoningFields merges reasoning lowering fields into the Chat request payload.
// Known reasoning-owned semantic fields and provider-private unknown reasoning carriers
// are allowed; known non-reasoning Chat semantic fields are rejected.
func applyReasoningFields(payload map[string]any, fields map[string]any) error {
	for k, v := range fields {
		trimmed := strings.ToLower(strings.TrimSpace(k))
		if _, forbidden := nonReasoningChatSemanticFields[trimmed]; forbidden {
			return canonical.InternalError(fmt.Sprintf("reasoning lowering illegally mutated non-reasoning semantic field %q", k))
		}
		payload[k] = v
	}
	return nil
}

// ApplyAttemptDecoration mutates the Chat request payload with non-semantic
// provider attempt decoration fields, rejecting collisions with standard Chat semantics.
func ApplyAttemptDecoration(payload map[string]any, fields map[string]any) error {
	for k, v := range fields {
		trimmed := strings.ToLower(strings.TrimSpace(k))
		if _, exists := payload[k]; exists {
			return canonical.InternalError(fmt.Sprintf("attempt decoration illegally mutated semantic field %q", k))
		}
		if _, reserved := reservedChatSemanticFields[trimmed]; reserved {
			return canonical.InternalError(fmt.Sprintf("attempt decoration illegally mutated semantic field %q", k))
		}
		payload[k] = v
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

func logChatCompletionsEncodeShape(req canonical.CanonicalRequest, tools []canonical.ToolDeclaration, wireMessages []ProviderRequestMessage, choice any, policy canonical.ToolPolicy, d delivery.Delivery) {
	instructions := chatCompletionsInstructionText(req.Items())
	toolChoiceMode := strings.TrimSpace(string(policy.Mode)) // swobu:io-string source=domain
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
		"tool_choice_shape", chatCompletionsWireToolChoiceShape(choice),
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

func chatCompletionsWireToolChoiceShape(choice any) string {
	switch choice.(type) {
	case nil:
		return "none"
	case string:
		return "mode"
	case map[string]any:
		return "object"
	}
	return "other"
}

// swobu:lint ignore string-switch because=protocol boundary encodes canonical tool-call kinds.
func encodeItems(items []canonical.CanonicalItem, tools []canonical.ToolDeclaration, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string, lowerMessage MessageLoweringRule) ([]ProviderRequestMessage, error) {
	var err error
	items, err = projectChatResponsesItems(items, changeLog, exchangeID)
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
			run, next, updatedBatch, err := encodeChatToolResultRun(items, i, activeBatch, changeLog, exchangeID)
			if err != nil {
				return nil, err
			}
			out = append(out, run...)
			activeBatch = updatedBatch
			i = next
			continue
		}
		if result, ok := item.ToolDiscoveryResult(); ok {
			if activeBatch == nil || !activeBatch.resolve(result.CallID()) {
				return nil, canonical.InternalError("tool-discovery result does not close an active tool-call batch")
			}
			content, err := encodeChatDiscoveryResult(result)
			if err != nil {
				return nil, err
			}
			out = append(out, ProviderRequestMessage{Role: "tool", Content: content, ToolCallID: result.CallID().String(), SourceStart: i, SourceEnd: i + 1})
			if activeBatch.closed() {
				activeBatch = nil
			}
			i++
			continue
		}
		wire := ProviderRequestMessage{SourceStart: sourceStart}
		if message, ok := item.Message(); ok {
			wire.Role = string(message.Role())
			content, err := encodeChatMessageContent(message.Role(), message.Content(), changeLog, exchangeID)
			if err != nil {
				return nil, err
			}
			wire.Content = content
			i++
		} else if item.Kind() == canonical.ItemKindToolCall {
			wire.Role = "assistant"
		} else {
			return nil, canonical.InternalError("Chat Completions received an invalid canonical item kind")
		}
		if wire.Role == "assistant" {
			callIDs := make([]canonical.ToolCallID, 0)
			for i < len(items) && items[i].Kind() == canonical.ItemKindToolCall {
				call, _ := items[i].ToolCall()
				encoded, err := encodeChatToolCall(call, names)
				if err != nil {
					return nil, err
				}
				wire.ToolCalls = append(wire.ToolCalls, encoded)
				callIDs = append(callIDs, call.CallID())
				i++
			}
			if len(callIDs) > 0 {
				if activeBatch != nil && !activeBatch.closed() {
					return nil, canonical.InternalError("canonical tool-call batches overlap")
				}
				activeBatch = newChatActiveToolBatch(callIDs)
			}
		}
		wire.SourceEnd = i
		if lowerMessage != nil {
			if err := lowerMessage(&wire, items); err != nil {
				return nil, err
			}
		}
		out = append(out, wire)
	}
	return out, nil
}

func projectChatResponsesItems(items []canonical.CanonicalItem, changeLog *[]compat.Change, exchangeID string) ([]canonical.CanonicalItem, error) {
	projected := make([]canonical.CanonicalItem, 0, len(items))
	for _, item := range items {
		if reasoning, ok := item.Reasoning(); ok {
			if _, present := reasoning.Opaque().Responses(); present {
				if err := appendChatRequestChange(changeLog, exchangeID, canonical.RequestItemsResponsesReasoningReplay, compat.Omission); err != nil {
					return nil, err
				}
			}
		}
		projected = append(projected, item)
	}
	return projected, nil
}

func encodeChatMessageContent(author canonical.MessageRole, parts []canonical.MessagePart, changeLog *[]compat.Change, exchangeID string) (any, error) {
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
				return nil, canonical.InternalError("Chat Completions received an invalid canonical image variant")
			}
			imagePart, _ := part.Image()
			encoded, err := encodeChatImage(imagePart, canonical.RequestItemsMessageImageDetail, changeLog, exchangeID)
			if err != nil {
				return nil, err
			}
			out = append(out, encoded)
			continue
		}
		return nil, canonical.InternalError("Chat Completions received an invalid canonical content variant")
	}
	return out, nil
}

func appendChatRequestChange(changeLog *[]compat.Change, exchangeID string, feature canonical.CapabilityPath, outcome compat.Kind) error {
	if changeLog == nil {
		return nil
	}
	change := compat.Change{Capability: feature, Kind: outcome}
	*changeLog = compat.AppendUnique(*changeLog, change)
	return nil
}

// swobu:lint ignore string-switch because=protocol boundary encodes canonical declaration kinds into chat-completions wire variants.
func encodeChatToolCall(call canonical.ToolCallItem, names wire.ToolNames) (toolCallBody, error) {
	tool := call.Tool()
	name, err := wire.EncodeToolName(names, tool)
	if err != nil {
		return toolCallBody{}, err
	}
	switch tool.Kind() {
	case canonical.ToolKindFunction, canonical.ToolKindDiscovery:
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
		return toolCallBody{}, canonical.InternalError("Chat Completions received an invalid canonical tool-call kind")
	}
}

func encodeChatDiscoveryResult(result canonical.ToolDiscoveryResultItem) (string, error) {
	if failure, failed := result.Failure(); failed {
		return failure.Message(), nil
	}
	loaded := make([]string, 0, len(result.Tools().Declarations()))
	for _, declaration := range result.Tools().Declarations() {
		loaded = append(loaded, declaration.Key().String())
	}
	raw, err := json.Marshal(map[string]any{"loaded_tools": loaded})
	if err != nil {
		return "", canonical.InternalError("chat completions discovery result could not be encoded")
	}
	return string(raw), nil
}

func chatClientTextContent(parts []canonical.MessagePart, surface string) (string, error) {
	var text strings.Builder
	for _, part := range parts {
		value, ok := part.Text()
		if !ok {
			return "", canonical.NewBackendError(
				"chat_completions",
				0,
				surface+" cannot represent the backend image response",
				"",
			)
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
			return "", canonical.InternalError(surface + " received an invalid canonical content variant")
		}
		text.WriteString(value.Text())
	}
	return text.String(), nil
}
