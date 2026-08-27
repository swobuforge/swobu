package messages

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

const (
	defaultMessagesMaxTokens      = 256
	toolSearchRegexType           = "tool_search_tool_regex_20251119"
	toolSearchRegexName           = "tool_search_tool_regex"
	toolSearchNaturalLanguageType = "tool_search_tool_bm25_20251119"
	toolSearchNaturalLanguageName = "tool_search_tool_bm25"
)

type messageBody struct {
	Role    string      `json:"role"`
	Content []contentID `json:"content"`
}

type contentID struct {
	Type      string                `json:"type"`
	Text      string                `json:"text,omitempty"`
	ID        string                `json:"id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Input     json.RawMessage       `json:"input,omitempty"`
	ToolUseID string                `json:"tool_use_id,omitempty"`
	IsError   bool                  `json:"is_error,omitempty"`
	Content   any                   `json:"content,omitempty"`
	Source    any                   `json:"source,omitempty"`
	Thinking  *string               `json:"thinking,omitempty"`
	Signature string                `json:"signature,omitempty"`
	Data      string                `json:"data,omitempty"`
	Citations []messagesCitationDTO `json:"citations,omitempty"`
	opaque    json.RawMessage
}

func (c contentID) MarshalJSON() ([]byte, error) {
	if len(c.opaque) > 0 {
		return append([]byte(nil), c.opaque...), nil
	}
	type contentIDAlias contentID
	return json.Marshal(contentIDAlias(c))
}

// ToolLoweringContext identifies one declaration during ordered lowering.
type ToolLoweringContext struct {
	Ordinal uint32
	Names   wire.ToolNames
}

// ToolLoweringRule replaces one semantic occurrence with zero or more target fragments.
type ToolLoweringRule func(ToolLoweringContext, canonical.ToolDeclaration) (fragments []ProviderRequestTool, handled bool, changes []compat.Change, err error)

// ToolPolicyLoweringRule resolves target policy after tool lowering.
type ToolPolicyLoweringRule func(canonical.ToolPolicy, wire.LoweredToolSet, wire.ToolNames) (choice any, handled bool, changes []compat.Change, err error)

// CompileOptions contains proven target-dialect extension points for Messages.
type CompileOptions struct {
	LowerTool            ToolLoweringRule
	LowerToolPolicy      ToolPolicyLoweringRule
	OmitAdaptiveThinking bool
}

// ProviderRequestDocument is the standard Messages lowering before any
// exact-provider typed dialect adaptation.
type ProviderRequestDocument struct {
	Payload map[string]any
	Tools   []ProviderRequestTool
}

func EncodeCarrierWithChanges(req canonical.CanonicalRequest, names wire.ToolNames, d delivery.Delivery, changeLog *[]compat.Change, exchangeID string) (carrier.Document, error) {
	document, err := CompileProviderRequestDocument(req, names, d, changeLog, exchangeID, CompileOptions{})
	if err != nil {
		return carrier.Document{}, err
	}
	return EncodeProviderRequestDocument(document)
}

// CompileProviderRequestDocument lowers one exact target dialect before the
// single serialization boundary.
func CompileProviderRequestDocument(req canonical.CanonicalRequest, names wire.ToolNames, d delivery.Delivery, changeLog *[]compat.Change, exchangeID string, options CompileOptions) (ProviderRequestDocument, error) {
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return ProviderRequestDocument{}, canonical.InternalError("Messages received an invalid delivery mode")
	}
	contextRejected, contextErr := projectMessagesResponsesReasoningContext(req.Reasoning(), changeLog, exchangeID)
	if contextErr != nil && !contextRejected {
		return ProviderRequestDocument{}, contextErr
	}
	items := req.Items()
	environment, err := canonical.ToolEnvironmentAt(items, len(items))
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	items, projectionDecisions, err := projectMessagesWebSearchLifecycles(items, canonical.RequestItemsKind)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if changeLog != nil {
		*changeLog = append(*changeLog, projectionDecisions...)
	}
	tools := environment.Declarations()
	flatTools, err := wire.PrepareFlatToolSet(tools, func(tool canonical.ToolDeclaration) (string, error) {
		if discovery, ok := tool.Discovery(); ok && discovery.Executor() == canonical.DiscoveryExecutorProvider {
			return tool.Key().Name(), nil
		}
		name, err := wire.EncodeToolName(names, tool.Key())
		return strings.TrimSpace(name), err
	})
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if flatTools.RemovedNamespaces > 0 {
		if err := appendMessagesRequestChange(changeLog, exchangeID, canonical.RequestTools, compat.Approximation); err != nil {
			return ProviderRequestDocument{}, err
		}
	}
	tools = flatTools.Declarations
	conversation, err := lowerMessagesContextPrefix(items, changeLog, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	wireMessages, err := encodeItems(conversation, tools, names, changeLog, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if contextErr != nil {
		return ProviderRequestDocument{}, contextErr
	}
	payload := map[string]any{
		"model":    req.Model(),
		"messages": wireMessages,
	}
	loweredInstructions := flattenInstructionsForMessages(items)
	if changeLog != nil {
		*changeLog = append(*changeLog, loweredInstructions.Changes...)
	}
	if loweredInstructions.Text != "" {
		payload["system"] = loweredInstructions.Text
	}
	deferred := messagesDeferredToolKeys(items)
	wireTools, loweredTools, err := compileMessagesTools(tools, deferred, names, changeLog, exchangeID, options.LowerTool)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if err := encodeMessagesGenerationControls(payload, req.Controls(), req.Reasoning()); err != nil {
		return ProviderRequestDocument{}, err
	}
	if err := encodeMessagesReasoning(payload, req.Reasoning(), options.OmitAdaptiveThinking, changeLog); err != nil {
		return ProviderRequestDocument{}, err
	}
	responseFormat, err := encodeMessagesOutputFormat(req.OutputFormat(), changeLog)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if len(responseFormat) > 0 {
		var format any
		if err := json.Unmarshal(responseFormat, &format); err != nil {
			return ProviderRequestDocument{}, canonical.InternalError("messages output format could not be materialized")
		}
		outputConfig, _ := payload["output_config"].(map[string]any)
		if outputConfig == nil {
			outputConfig = map[string]any{}
		}
		outputConfig["format"] = format
		payload["output_config"] = outputConfig
		if err := appendMessagesRequestChange(changeLog, exchangeID, canonical.RequestOutputFormat, compat.Approximation); err != nil {
			return ProviderRequestDocument{}, err
		}
	}
	policy, err := req.EffectiveToolPolicy()
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
			choice, err = encodeMessagesToolChoice(policy, loweredTools, names, changeLog, exchangeID)
		}
	} else {
		choice, err = encodeMessagesToolChoice(policy, loweredTools, names, changeLog, exchangeID)
	}
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	choice, err = encodeMessagesToolCallBatch(choice, req.ToolCallBatch(), loweredTools.TotalFragments() > 0)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if choice != nil {
		payload["tool_choice"] = choice
	}
	if d.Mode == delivery.Streaming {
		payload["stream"] = true
	}
	return ProviderRequestDocument{Payload: payload, Tools: wireTools}, nil
}

func lowerMessagesContextPrefix(items []canonical.CanonicalItem, changeLog *[]compat.Change, exchangeID string) ([]canonical.CanonicalItem, error) {
	conversation := make([]canonical.CanonicalItem, 0, len(items))
	historyStarted := false
	for _, item := range items {
		if message, ok := item.Message(); ok &&
			(message.Role() == canonical.MessageRoleSystem || message.Role() == canonical.MessageRoleDeveloper) {
			if historyStarted {
				if err := appendMessagesRequestChange(changeLog, exchangeID, canonical.RequestInstructions, compat.Approximation); err != nil {
					return nil, err
				}
			}
			continue
		}
		if _, declarations := item.ToolDeclarations(); declarations {
			if historyStarted {
				if err := appendMessagesRequestChange(changeLog, exchangeID, canonical.RequestTools, compat.Approximation); err != nil {
					return nil, err
				}
			}
			continue
		}
		historyStarted = true
		conversation = append(conversation, item)
	}
	return conversation, nil
}

// projectMessagesResponsesReasoningContext owns the known standard-grammar
// mismatch without changing portable reasoning or current request history.
func projectMessagesResponsesReasoningContext(reasoning canonical.ReasoningControls, changeLog *[]compat.Change, exchangeID string) (bool, error) {
	if !reasoning.ResponsesContextField().IsSpecified() {
		return false, nil
	}
	if err := appendMessagesRequestChange(changeLog, exchangeID, canonical.RequestReasoningContextResponses, compat.Omission); err != nil {
		return false, err
	}
	return false, nil
}

// EncodeProviderRequestDocument performs the single serialization boundary
// after standard lowering or exact-provider typed composition.
func EncodeProviderRequestDocument(document ProviderRequestDocument) (carrier.Document, error) {
	if len(document.Tools) > 0 {
		document.Payload["tools"] = document.Tools
	} else {
		delete(document.Payload, "tools")
	}
	raw, err := json.Marshal(document.Payload)
	if err != nil {
		return carrier.Document{}, canonical.BadRequest("conversation request could not be encoded for the messages protocol")
	}
	return carrier.NewDocument(
		"",
		"application/json",
		nil,
		raw,
		carrier.Meta{},
	), nil
}

func encodeItems(items []canonical.CanonicalItem, tools []canonical.ToolDeclaration, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string) ([]messageBody, error) {
	if len(items) == 0 {
		return nil, canonical.BadRequest("messages protocol requires at least one canonical item")
	}
	var err error
	items, err = projectMessagesResponsesItems(items, changeLog, exchangeID)
	if err != nil {
		return nil, err
	}
	out := make([]messageBody, 0, len(items))
	for i := 0; i < len(items); {
		owner := items[i].Owner()
		if owner != canonical.TurnOwnerUser && owner != canonical.TurnOwnerAssistant {
			return nil, canonical.NotImplemented("Messages cannot project interleaved canonical system or developer messages")
		}
		wire := messageBody{Role: string(owner)}
		for i < len(items) && items[i].Owner() == owner {
			var err error
			wire.Content, err = appendMessagesItemBlocks(wire.Content, items[i], tools, names, owner, changeLog, exchangeID)
			if err != nil {
				return nil, err
			}
			i++
		}
		if len(wire.Content) > 0 {
			out = append(out, wire)
		}
	}
	return out, nil
}

func projectMessagesResponsesItems(items []canonical.CanonicalItem, changeLog *[]compat.Change, exchangeID string) ([]canonical.CanonicalItem, error) {
	projected := make([]canonical.CanonicalItem, 0, len(items))
	for _, item := range items {
		if reasoning, ok := item.Reasoning(); ok {
			if _, present := reasoning.Opaque().Responses(); present {
				if err := appendMessagesRequestChange(changeLog, exchangeID, canonical.RequestItemsResponsesReasoningReplay, compat.Omission); err != nil {
					return nil, err
				}
			}
		}
		projected = append(projected, item)
	}
	return projected, nil
}

func appendMessagesItemBlocks(blocks []contentID, item canonical.CanonicalItem, tools []canonical.ToolDeclaration, names wire.ToolNames, owner canonical.TurnOwner, changeLog *[]compat.Change, exchangeID string) ([]contentID, error) {
	if message, ok := item.Message(); ok {
		for _, part := range message.Content() {
			if text, ok := part.Text(); ok {
				citations, err := encodeMessagesCitations(text.Text(), part.Citations())
				if err != nil {
					return nil, err
				}
				blocks = append(blocks, contentID{Type: "text", Text: text.Text(), Citations: citations})
				continue
			}
			if owner != canonical.TurnOwnerUser {
				return nil, canonical.NotImplemented("Messages cannot project image input outside user messages")
			}
			image, ok := part.Image()
			if !ok {
				return nil, canonical.NotImplemented("Messages cannot project this canonical content kind")
			}
			block, err := encodeMessagesImage(image, changeLog, exchangeID, canonical.RequestItemsMessageImageDetail)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		}
		return blocks, nil
	}
	if item.Kind() == canonical.ItemKindToolCall {
		block, err := encodeMessagesToolCall(item, tools, names)
		if err != nil {
			return nil, err
		}
		return append(blocks, block), nil
	}
	if result, ok := item.ToolResult(); ok {
		if search, ok := result.WebSearch(); ok {
			content, err := encodeMessagesWebSearchResult(search)
			if err != nil {
				return nil, err
			}
			return append(blocks, contentID{Type: "web_search_tool_result", ToolUseID: result.CallID().String(), Content: content}), nil
		}
		content, err := encodeMessagesToolResultContent(result.Content(), changeLog, exchangeID)
		if err != nil {
			return nil, err
		}
		return append(blocks, contentID{Type: "tool_result", ToolUseID: result.CallID().String(), Content: content, IsError: result.IsError()}), nil
	}
	if result, ok := item.ToolDiscoveryResult(); ok {
		if failure, failed := result.Failure(); failed {
			if result.Executor() == canonical.DiscoveryExecutorClient {
				return append(blocks, contentID{Type: "tool_result", ToolUseID: result.CallID().String(), Content: failure.Message(), IsError: true}), nil
			}
			code, _ := failure.Code().Get()
			return append(blocks, contentID{Type: "tool_search_tool_result", ToolUseID: result.CallID().String(), Content: map[string]any{
				"type": "tool_search_tool_result_error", "error_code": code, "error_message": failure.Message(),
			}}), nil
		}
		content := make([]map[string]string, 0, len(result.Tools().Declarations()))
		for _, declaration := range result.Tools().Declarations() {
			name, err := wire.EncodeToolName(names, declaration.Key())
			if err != nil {
				return nil, err
			}
			content = append(content, map[string]string{"type": "tool_reference", "tool_name": name})
		}
		if result.Executor() == canonical.DiscoveryExecutorClient {
			return append(blocks, contentID{Type: "tool_result", ToolUseID: result.CallID().String(), Content: content}), nil
		}
		return append(blocks, contentID{Type: "tool_search_tool_result", ToolUseID: result.CallID().String(), Content: map[string]any{
			"type": "tool_search_tool_search_result", "tool_references": content,
		}}), nil
	}
	if reasoning, ok := item.Reasoning(); ok {
		opaque, exact := reasoning.Opaque().Messages()
		if !exact {
			return blocks, nil
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(opaque, &envelope); err != nil || envelope.Type != "thinking" && envelope.Type != "redacted_thinking" {
			return nil, canonical.InternalError("messages opaque thinking is invalid")
		}
		return append(blocks, contentID{opaque: opaque}), nil
	}
	return nil, canonical.NotImplemented("Messages cannot project this canonical item kind")
}

func encodeMessagesToolCall(item canonical.CanonicalItem, tools []canonical.ToolDeclaration, names wire.ToolNames) (contentID, error) {
	call, ok := item.ToolCall()
	if !ok {
		return contentID{}, canonical.InternalError("messages tool-call item is invalid")
	}
	tool := call.Tool()
	if tool.Kind() == canonical.ToolKindWebSearch {
		search, ok := call.Input().WebSearch()
		if !ok || search.Action != canonical.WebSearchActionSearch || len(search.Queries) != 1 {
			return contentID{}, canonical.NotImplemented("Messages cannot project multi-query canonical server-tool calls")
		}
		input, err := json.Marshal(map[string]string{"query": search.Queries[0]})
		if err != nil {
			return contentID{}, canonical.InternalError("messages web-search call could not be encoded")
		}
		return contentID{Type: "server_tool_use", ID: call.CallID().String(), Name: "web_search", Input: input}, nil
	}
	if tool.Kind() == canonical.ToolKindDiscovery {
		executor, ok := call.DiscoveryExecutor()
		if !ok {
			return contentID{}, canonical.InternalError("messages discovery call is missing execution owner")
		}
		object, ok := call.Input().Object()
		if !ok {
			return contentID{}, canonical.InternalError("canonical discovery call input is not an object")
		}
		if executor == canonical.DiscoveryExecutorClient {
			name, err := wire.EncodeToolName(names, tool)
			if err != nil {
				return contentID{}, err
			}
			return contentID{Type: "tool_use", ID: call.CallID().String(), Name: name, Input: json.RawMessage(object.Bytes())}, nil
		}
		discovery, err := messagesDiscoveryDeclaration(tools)
		if err != nil {
			return contentID{}, err
		}
		name, err := messagesProviderDiscoveryName(discovery)
		if err != nil {
			return contentID{}, err
		}
		return contentID{Type: "server_tool_use", ID: call.CallID().String(), Name: name, Input: json.RawMessage(object.Bytes())}, nil
	}
	if tool.Kind() != canonical.ToolKindFunction {
		return contentID{}, canonical.NotImplemented("Messages cannot project this canonical tool-call kind")
	}
	name, err := wire.EncodeToolName(names, tool)
	if err != nil {
		return contentID{}, err
	}
	object, ok := call.Input().Object()
	if !ok {
		return contentID{}, canonical.BadRequest("messages function tool calls require object input")
	}
	return contentID{Type: "tool_use", ID: call.CallID().String(), Name: name, Input: json.RawMessage(object.Bytes())}, nil
}

func messagesTextOnlyContent(parts []canonical.MessagePart, surface string) (string, error) {
	var builder strings.Builder
	for _, part := range parts {
		text, ok := part.Text()
		if !ok {
			return "", canonical.NotImplemented(surface + " cannot project this canonical content kind")
		}
		builder.WriteString(text.Text())
	}
	return builder.String(), nil
}

func encodeMessagesToolResultContent(parts []canonical.ToolResultPart, changeLog *[]compat.Change, exchangeID string) (any, error) {
	if len(parts) == 1 {
		if text, ok := parts[0].Text(); ok {
			return text.Text(), nil
		}
	}
	content := make([]contentID, 0, len(parts))
	for _, part := range parts {
		if text, ok := part.Text(); ok {
			content = append(content, contentID{Type: "text", Text: text.Text()})
			continue
		}
		image, ok := part.Image()
		if !ok {
			return nil, canonical.NotImplemented("Messages cannot project this canonical tool-result content kind")
		}
		block, err := encodeMessagesImage(image, changeLog, exchangeID, canonical.RequestItemsToolResultImageDetail)
		if err != nil {
			return nil, err
		}
		content = append(content, block)
	}
	return content, nil
}

func encodeMessagesImage(image canonical.ImagePart, changeLog *[]compat.Change, exchangeID string, detailFeature canonical.CapabilityPath) (contentID, error) {
	if image.Detail().IsSpecified() {
		if err := appendMessagesRequestChange(changeLog, exchangeID, detailFeature, compat.Approximation); err != nil {
			return contentID{}, err
		}
	}
	source := image.Source()
	if rawURL, ok := source.URL(); ok {
		return contentID{Type: "image", Source: map[string]string{"type": "url", "url": rawURL.String()}}, nil
	}
	if inline, ok := source.Inline(); ok {
		return contentID{Type: "image", Source: map[string]string{"type": "base64", "media_type": string(inline.MediaType()), "data": base64.StdEncoding.EncodeToString(inline.Data())}}, nil
	}
	return contentID{}, canonical.InternalError("canonical image source is invalid")
}

func appendMessagesRequestChange(changeLog *[]compat.Change, exchangeID string, feature canonical.CapabilityPath, outcome compat.Kind) error {
	if changeLog == nil {
		return nil
	}
	change := compat.Change{Capability: feature, Kind: outcome}
	if outcome == compat.Approximation {
		change.Preserved = feature
	}
	*changeLog = compat.AppendUnique(*changeLog, change)
	return nil
}

func encodeMessagesTools(tools []canonical.ToolDeclaration, deferred map[canonical.ToolKey]struct{}, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string) ([]ProviderRequestTool, error) {
	typed, _, err := compileMessagesTools(tools, deferred, names, changeLog, exchangeID, nil)
	return typed, err
}

func compileMessagesTools(tools []canonical.ToolDeclaration, deferred map[canonical.ToolKey]struct{}, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string, rule ToolLoweringRule) ([]ProviderRequestTool, wire.LoweredToolSet, error) {
	if len(tools) == 0 {
		return nil, wire.LoweredToolSet{}, nil
	}
	for _, tool := range tools {
		if decl, ok := tool.Function(); ok {
			if strict, specified := decl.Strict().Get(); specified && strict {
				if err := appendMessagesRequestChange(changeLog, exchangeID, canonical.RequestToolsSchemaStrict, compat.Omission); err != nil {
					return nil, wire.LoweredToolSet{}, err
				}
				break
			}
		}
	}
	out := make([]ProviderRequestTool, 0, len(tools))
	lowered := wire.LoweredToolSet{Records: make([]wire.LoweredToolRecord, 0, len(tools))}
	for ordinal, tool := range tools {
		if rule != nil {
			fragments, handled, changes, err := rule(ToolLoweringContext{Ordinal: uint32(ordinal), Names: names}, tool)
			if changeLog != nil {
				*changeLog = append(*changeLog, changes...)
			}
			if err != nil {
				return nil, wire.LoweredToolSet{}, err
			}
			if handled {
				for _, fragment := range fragments {
					_, fragment.DeferLoading = deferred[tool.Key()]
					out = append(out, fragment)
				}
				lowered.Records = append(lowered.Records, wire.LoweredToolRecord{
					Key:           tool.Key(),
					Kind:          tool.Kind(),
					FragmentCount: len(fragments),
				})
				continue
			}
		}
		if decl, ok := tool.Function(); ok {
			wireTool, err := encodeMessagesFunctionTool(tool, decl, names)
			if err != nil {
				return nil, wire.LoweredToolSet{}, err
			}
			_, wireTool.DeferLoading = deferred[tool.Key()]
			out = append(out, wireTool)
			lowered.Records = append(lowered.Records, wire.LoweredToolRecord{
				Key:           tool.Key(),
				Kind:          tool.Kind(),
				FragmentCount: 1,
			})
			continue
		}
		if discovery, ok := tool.Discovery(); ok {
			if discovery.Executor() == canonical.DiscoveryExecutorClient {
				schema, err := messagesToolSchema(discovery.InputSchema())
				if err != nil {
					return nil, wire.LoweredToolSet{}, err
				}
				name, err := wire.EncodeToolName(names, tool.Key())
				if err != nil {
					return nil, wire.LoweredToolSet{}, err
				}
				wireTool := ProviderRequestTool{Name: name, Description: discovery.Description(), InputSchema: schema}
				out = append(out, wireTool)
				lowered.Records = append(lowered.Records, wire.LoweredToolRecord{
					Key:           tool.Key(),
					Kind:          tool.Kind(),
					FragmentCount: 1,
				})
				continue
			}
			typeName, name, representable := messagesProviderDiscoveryTool(discovery)
			if !representable {
				if changeLog != nil {
					*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(tool.Key())))
				}
				lowered.Records = append(lowered.Records, wire.LoweredToolRecord{Key: tool.Key(), Kind: tool.Kind()})
				continue
			}
			wireTool := ProviderRequestTool{Type: typeName, Name: name}
			out = append(out, wireTool)
			lowered.Records = append(lowered.Records, wire.LoweredToolRecord{
				Key:           tool.Key(),
				Kind:          tool.Kind(),
				FragmentCount: 1,
			})
			continue
		}
		if tool.Kind() == canonical.ToolKindWebSearch {
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(tool.Key())))
			}
			lowered.Records = append(lowered.Records, wire.LoweredToolRecord{Key: tool.Key(), Kind: tool.Kind()})
			continue
		}
		if changeLog != nil {
			*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(tool.Key())))
		}
		lowered.Records = append(lowered.Records, wire.LoweredToolRecord{Key: tool.Key(), Kind: tool.Kind()})
	}
	if len(out) > 0 && len(deferred) > 0 {
		allDeferred := true
		for _, tool := range out {
			if !tool.DeferLoading {
				allDeferred = false
				break
			}
		}
		if allDeferred {
			out[0].DeferLoading = false
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewApproximation(canonical.RequestToolsVisibility, canonical.RequestTools, canonical.Occurrence{}))
			}
		}
	}
	return out, lowered, nil
}

func messagesDeferredToolKeys(items []canonical.CanonicalItem) map[canonical.ToolKey]struct{} {
	deferred := make(map[canonical.ToolKey]struct{})
	for _, item := range items {
		if declarations, ok := item.ToolDeclarations(); ok {
			for _, key := range declarations.Visibility().DeferredKeys() {
				deferred[key] = struct{}{}
			}
		}
		if result, ok := item.ToolDiscoveryResult(); ok {
			for _, declaration := range result.Tools().Declarations() {
				if messagesToolCanBeDeferred(declaration.Kind()) {
					deferred[declaration.Key()] = struct{}{}
				}
			}
			for _, key := range result.Visibility().DeferredKeys() {
				deferred[key] = struct{}{}
			}
		}
	}
	return deferred
}

func messagesDiscoveryDeclaration(tools []canonical.ToolDeclaration) (canonical.ToolDiscoveryTool, error) {
	for _, declaration := range tools {
		if discovery, ok := declaration.Discovery(); ok {
			return discovery, nil
		}
	}
	return canonical.ToolDiscoveryTool{}, canonical.InternalError("messages discovery declaration is missing")
}

func messagesProviderDiscoveryTool(discovery canonical.ToolDiscoveryTool) (string, string, bool) {
	switch discovery.QueryKind() {
	case canonical.ToolDiscoveryQueryRegex:
		return toolSearchRegexType, toolSearchRegexName, true
	case canonical.ToolDiscoveryQueryNaturalLanguage:
		return toolSearchNaturalLanguageType, toolSearchNaturalLanguageName, true
	default:
		return "", "", false
	}
}

func messagesProviderDiscoveryName(discovery canonical.ToolDiscoveryTool) (string, error) {
	switch discovery.QueryKind() {
	case canonical.ToolDiscoveryQueryRegex:
		return toolSearchRegexName, nil
	case canonical.ToolDiscoveryQueryNaturalLanguage:
		return toolSearchNaturalLanguageName, nil
	default:
		return "", canonical.InternalError("Messages provider discovery lowering admitted an unsupported query kind")
	}
}

func encodeMessagesFunctionTool(declaration canonical.ToolDeclaration, decl canonical.FunctionTool, names wire.ToolNames) (ProviderRequestTool, error) {
	schema, err := messagesToolSchema(decl.InputSchema())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	name, err := wire.EncodeToolName(names, declaration.Key())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return ProviderRequestTool{}, canonical.BadRequest("messages protocol tool declarations require a name")
	}
	return ProviderRequestTool{
		Name:        name,
		Description: strings.TrimSpace(decl.Description()), // swobu:io-string source=boundary
		InputSchema: schema,
	}, nil
}

func messagesToolSchema(schema canonical.ToolSchema) (json.RawMessage, error) {
	raw := strings.TrimSpace(schema.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, canonical.BadRequest("messages protocol tool declarations require input_schema")
	}
	obj, err := sse.DecodeJSONObject(json.RawMessage(raw), "messages protocol tool declaration input_schema is invalid")
	if err != nil {
		return nil, err
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return nil, canonical.InternalError("messages protocol tool declarations could not be encoded")
	}
	return json.RawMessage(normalized), nil
}

func messagesToolSchemaFromWire(raw json.RawMessage) (canonical.ToolSchema, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return canonical.ToolSchema{}, canonical.BadRequest("messages request tool declarations require input_schema")
	}
	obj, err := sse.DecodeJSONObject(json.RawMessage(trimmed), "messages request tool declaration input_schema is invalid")
	if err != nil {
		return canonical.ToolSchema{}, err
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return canonical.ToolSchema{}, canonical.InternalError("messages request tool declarations could not be decoded")
	}
	object, err := canonical.ParseJSONObject(normalized)
	if err != nil {
		return canonical.ToolSchema{}, canonical.BadRequest("messages request tool declaration input_schema is invalid")
	}
	return canonical.NewToolSchemaObject(object), nil
}

var reservedMessagesSemanticFields = map[string]struct{}{
	"model": {}, "messages": {}, "system": {}, "tools": {}, "tool_choice": {},
	"stream": {}, "temperature": {}, "top_p": {}, "top_k": {}, "max_tokens": {},
	"stop_sequences": {}, "thinking": {}, "output_config": {}, "metadata": {},
}

// ApplyAttemptDecoration mutates the Messages request payload with non-semantic
// provider attempt decoration fields, rejecting collisions with standard Messages semantics.
func ApplyAttemptDecoration(payload map[string]any, fields map[string]any) error {
	for k, v := range fields {
		trimmed := strings.ToLower(strings.TrimSpace(k))
		if _, exists := payload[k]; exists {
			return canonical.InternalError(fmt.Sprintf("attempt decoration illegally mutated semantic field %q", k))
		}
		if _, reserved := reservedMessagesSemanticFields[trimmed]; reserved {
			return canonical.InternalError(fmt.Sprintf("attempt decoration illegally mutated semantic field %q", k))
		}
		payload[k] = v
	}
	return nil
}
