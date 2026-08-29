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

// ToolTransformer totally projects one semantic tool slot.
type ToolProjection struct {
	Fragments     []ProviderRequestTool
	TargetType    string
	TargetName    string
	ProjectCall   func(canonical.ToolCallItem) (ToolCallProjection, error)
	ProjectResult func(canonical.ToolResultItem) (ToolResultProjection, error)
}

// ToolCallProjection is a complete Messages call block selected by one tool
// slot. History serialization copies this value; it does not choose a carrier
// or reinterpret canonical tool kinds.
type ToolCallProjection struct {
	Type  string
	Name  string
	Input json.RawMessage
}

// ToolResultProjection is the closed Messages result carrier selected by the
// same occurrence projection as its declaration and call.
type ToolResultProjection struct {
	Type string
}

type ToolTransformer func(ToolLoweringContext, canonical.ToolDeclaration) (ToolProjection, []compat.Change, error)

// compiledToolProjection pairs inert emitted provenance with typed Messages
// behavior retained for each declaration occurrence.
type compiledToolProjection struct {
	lowered     wire.LoweredToolSet
	occurrences map[canonical.ToolKey]ToolProjection
}

// ToolLowering is the resolved Messages tool algebra.
type ToolLowering struct {
	Function, Custom, WebSearch, Discovery ToolTransformer
}

// ReasoningTransformer totally projects canonical reasoning controls into the
// Messages request payload.
type ReasoningTransformer func(payload map[string]any, reasoning canonical.ReasoningControls, changeLog *[]compat.Change) error

// Lowering is the resolved Messages semantic algebra. Every slot is total
// before request encoding begins.
type Lowering struct {
	Tools     ToolLowering
	Reasoning ReasoningTransformer
}

// Overlay replaces only explicitly supplied semantic slots.
func (l Lowering) Overlay(override Lowering) Lowering {
	l.Tools = l.Tools.Overlay(override.Tools)
	if override.Reasoning != nil {
		l.Reasoning = override.Reasoning
	}
	return l
}

// Overlay replaces only explicitly supplied slots.
func (l ToolLowering) Overlay(override ToolLowering) ToolLowering {
	if override.Function != nil {
		l.Function = override.Function
	}
	if override.Custom != nil {
		l.Custom = override.Custom
	}
	if override.WebSearch != nil {
		l.WebSearch = override.WebSearch
	}
	if override.Discovery != nil {
		l.Discovery = override.Discovery
	}
	return l
}

// ProtocolLowering returns the total Messages protocol baseline. Provider
// construction overlays only proven divergences onto this value.
func ProtocolLowering() Lowering {
	return DefaultLowering()
}

// CompileOptions contains proven target-dialect extension points for Messages.
type CompileOptions struct {
	Lowering Lowering
}

// ProviderRequestDocument is the standard Messages lowering before any
// exact-provider typed dialect adaptation.
type ProviderRequestDocument struct {
	Payload map[string]any
	Tools   []ProviderRequestTool
}

func EncodeCarrierWithChanges(req canonical.CanonicalRequest, names wire.ToolNames, d delivery.Delivery, changeLog *[]compat.Change, exchangeID string) (carrier.Document, error) {
	document, err := CompileProviderRequestDocument(req, names, d, changeLog, exchangeID, CompileOptions{Lowering: DefaultLowering()})
	if err != nil {
		return carrier.Document{}, err
	}
	return EncodeProviderRequestDocument(document)
}

// CompileProviderRequestDocument lowers one exact target dialect before the
// single serialization boundary.
func CompileProviderRequestDocument(req canonical.CanonicalRequest, names wire.ToolNames, d delivery.Delivery, changeLog *[]compat.Change, exchangeID string, options CompileOptions) (ProviderRequestDocument, error) {
	lowering := options.Lowering
	if !lowering.resolved() {
		return ProviderRequestDocument{}, canonical.InternalError("Messages compile requires resolved lowering")
	}
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
	tools = removeEagerProviderDiscovery(flatTools.Declarations)
	deferred := messagesDeferredToolKeys(items)
	if hasEagerProviderDiscovery(flatTools.Declarations) {
		deferred = nil
		if err := appendMessagesRequestChange(changeLog, exchangeID, canonical.RequestToolsVisibility, compat.Approximation); err != nil {
			return ProviderRequestDocument{}, err
		}
	}
	wireTools, toolProjection, err := compileMessagesTools(tools, deferred, names, changeLog, exchangeID, lowering.Tools)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	toolProjection, err = projectMessagesHistoricalTools(items, toolProjection, lowering.Tools, names)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	items, historyChanges, err := projectMessagesUnloweredToolHistory(items, toolProjection.lowered)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if changeLog != nil {
		*changeLog = append(*changeLog, historyChanges...)
	}
	conversation, err := lowerMessagesContextPrefix(items, changeLog, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	wireMessages, err := encodeItems(conversation, tools, names, toolProjection, changeLog, exchangeID)
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
	policy, err := req.EffectiveToolPolicy()
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if err := encodeMessagesGenerationControls(payload, req.Controls(), req.Reasoning()); err != nil {
		return ProviderRequestDocument{}, err
	}
	if err := lowering.Reasoning(payload, req.Reasoning(), changeLog); err != nil {
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
	choice, err := encodeMessagesToolChoice(policy, toolProjection.lowered, names, changeLog, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	choice, err = encodeMessagesToolCallBatch(choice, req.ToolCallBatch(), toolProjection.lowered.TotalFragments() > 0)
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

func (l Lowering) resolved() bool {
	return l.Tools.resolved() && l.Reasoning != nil
}

func (l ToolLowering) resolved() bool {
	return l.Function != nil && l.Custom != nil && l.WebSearch != nil && l.Discovery != nil
}

// projectMessagesHistoricalTools executes the selected semantic slot for the
// one declaration-free tool kind admitted by Messages replay. It never repairs
// or reinterprets an existing projection; ordinary callable history still
// requires retained declaration provenance.
func projectMessagesHistoricalTools(items []canonical.CanonicalItem, compiled compiledToolProjection, lowering ToolLowering, names wire.ToolNames) (compiledToolProjection, error) {
	for _, item := range items {
		call, ok := item.ToolCall()
		if !ok || call.Tool().Kind() != canonical.ToolKindWebSearch {
			continue
		}
		if _, found := compiled.lowered.FindSource(call.Tool()); found {
			continue
		}
		declaration := canonical.NewWebSearchDeclaration()
		projection, _, err := lowering.WebSearch(ToolLoweringContext{Names: names}, declaration)
		if err != nil {
			return compiledToolProjection{}, err
		}
		compiled.lowered.Records = append(compiled.lowered.Records, wire.LoweredToolRecord{
			Key: call.Tool(), Kind: call.Tool().Kind(), FragmentCount: len(projection.Fragments),
			TargetType: projection.TargetType, TargetName: projection.TargetName,
		})
		if compiled.occurrences == nil {
			compiled.occurrences = make(map[canonical.ToolKey]ToolProjection)
		}
		compiled.occurrences[call.Tool()] = projection
	}
	return compiled, nil
}

// projectMessagesUnloweredToolHistory keeps declaration and history projection
// on one emitted-fragment authority. A zero-fragment callable effect is omitted
// atomically; one historical call cannot truthfully name multiple fragments.
func projectMessagesUnloweredToolHistory(items []canonical.CanonicalItem, lowered wire.LoweredToolSet) ([]canonical.CanonicalItem, []compat.Change, error) {
	effects, err := canonical.MatchToolEffects(items)
	if err != nil {
		// Isolated wire-encoder tests may exercise a result without materialized
		// history. The canonical request boundary rejects that state; preserve the
		// local item here so its owning encoder can still be tested directly.
		return append([]canonical.CanonicalItem(nil), items...), nil, nil
	}
	drop := make(map[int]struct{})
	changes := make([]compat.Change, 0)
	for _, effect := range effects {
		call, ok := items[effect.CallIndex].ToolCall()
		if !ok || call.Tool().Kind() == canonical.ToolKindWebSearch {
			continue
		}
		record, found := lowered.FindSource(call.Tool())
		fragments := 0
		if found {
			fragments = record.FragmentCount
		}
		switch {
		case fragments == 1:
			continue
		case fragments > 1:
			return nil, nil, canonical.InternalError("Messages callable history source lowered to multiple wire call identities")
		default:
			drop[effect.CallIndex] = struct{}{}
			if effect.ResultIndex >= 0 {
				drop[effect.ResultIndex] = struct{}{}
			}
			changes = append(changes, compat.NewOmission(canonical.RequestItemsKind, canonical.RequestItemOccurrence(uint32(effect.CallIndex))))
		}
	}
	if len(drop) == 0 {
		return append([]canonical.CanonicalItem(nil), items...), nil, nil
	}
	projected := make([]canonical.CanonicalItem, 0, len(items)-len(drop))
	for index, item := range items {
		if _, omitted := drop[index]; !omitted {
			projected = append(projected, item)
		}
	}
	return projected, changes, nil
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

func encodeItems(items []canonical.CanonicalItem, tools []canonical.ToolDeclaration, names wire.ToolNames, projection compiledToolProjection, changeLog *[]compat.Change, exchangeID string) ([]messageBody, error) {
	if len(items) == 0 {
		return nil, canonical.BadRequest("messages protocol requires at least one canonical item")
	}
	var err error
	items, err = projectMessagesResponsesItems(items, changeLog, exchangeID)
	if err != nil {
		return nil, err
	}
	resultRecords := messagesResultProjectionQueues(items, projection)
	out := make([]messageBody, 0, len(items))
	for i := 0; i < len(items); {
		owner := items[i].Owner()
		if owner != canonical.TurnOwnerUser && owner != canonical.TurnOwnerAssistant {
			return nil, canonical.InternalError("Messages received an invalid canonical system/developer item")
		}
		wire := messageBody{Role: string(owner)}
		for i < len(items) && items[i].Owner() == owner {
			var err error
			wire.Content, err = appendMessagesItemBlocks(wire.Content, items[i], tools, names, projection, resultRecords, owner, changeLog, exchangeID)
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

func messagesResultProjectionQueues(items []canonical.CanonicalItem, projection compiledToolProjection) map[canonical.ToolCallID][]ToolProjection {
	queues := make(map[canonical.ToolCallID][]ToolProjection)
	effects, err := canonical.MatchToolEffects(items)
	if err != nil {
		return queues
	}
	for _, effect := range effects {
		if effect.ResultIndex < 0 {
			continue
		}
		call, ok := items[effect.CallIndex].ToolCall()
		if !ok {
			continue
		}
		if occurrence, found := projection.occurrences[call.Tool()]; found {
			queues[effect.CallID] = append(queues[effect.CallID], occurrence)
		}
	}
	return queues
}

func popMessagesResultProjection(queues map[canonical.ToolCallID][]ToolProjection, callID canonical.ToolCallID) (ToolProjection, bool) {
	records := queues[callID]
	if len(records) == 0 {
		return ToolProjection{}, false
	}
	queues[callID] = records[1:]
	return records[0], true
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

func appendMessagesItemBlocks(blocks []contentID, item canonical.CanonicalItem, tools []canonical.ToolDeclaration, names wire.ToolNames, projection compiledToolProjection, resultRecords map[canonical.ToolCallID][]ToolProjection, owner canonical.TurnOwner, changeLog *[]compat.Change, exchangeID string) ([]contentID, error) {
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
				return nil, canonical.InternalError("Messages received an invalid canonical image variant")
			}
			image, ok := part.Image()
			if !ok {
				return nil, canonical.InternalError("Messages received an invalid canonical content variant")
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
		block, err := encodeMessagesToolCall(item, projection)
		if err != nil {
			return nil, err
		}
		return append(blocks, block), nil
	}
	if result, ok := item.ToolResult(); ok {
		record, projected := popMessagesResultProjection(resultRecords, result.CallID())
		resultType := "tool_result"
		if projected {
			if record.ProjectResult == nil {
				return nil, canonical.InternalError("Messages tool result has no emitted target carrier")
			}
			value, err := record.ProjectResult(result)
			if err != nil {
				return nil, err
			}
			resultType = value.Type
			if resultType == "" {
				return nil, canonical.InternalError("Messages tool result projection returned an invalid carrier")
			}
		}
		if search, ok := result.WebSearch(); ok {
			if resultType != "web_search_tool_result" {
				return nil, canonical.InternalError("Messages web-search result has no emitted target carrier")
			}
			content, err := encodeMessagesWebSearchResult(search)
			if err != nil {
				return nil, err
			}
			return append(blocks, contentID{Type: "web_search_tool_result", ToolUseID: result.CallID().String(), Content: content}), nil
		}
		if resultType != "tool_result" {
			return nil, canonical.InternalError("Messages tool result has no emitted target carrier")
		}
		content, err := encodeMessagesToolResultContent(result.Content(), changeLog, exchangeID)
		if err != nil {
			return nil, err
		}
		return append(blocks, contentID{Type: "tool_result", ToolUseID: result.CallID().String(), Content: content, IsError: result.IsError()}), nil
	}
	if result, ok := item.ToolDiscoveryResult(); ok {
		record, projected := popMessagesResultProjection(resultRecords, result.CallID())
		resultType := "tool_result"
		if projected {
			if record.ProjectResult == nil {
				return nil, canonical.InternalError("Messages discovery result has no emitted target carrier")
			}
			value, err := record.ProjectResult(canonical.ToolResultItem{})
			if err != nil {
				return nil, err
			}
			resultType = value.Type
			if resultType == "" {
				return nil, canonical.InternalError("Messages discovery result projection returned an invalid carrier")
			}
		}
		if resultType != "tool_result" && resultType != "tool_search_tool_result" {
			return nil, canonical.InternalError("Messages discovery result has no emitted target carrier")
		}
		if failure, failed := result.Failure(); failed {
			if resultType == "tool_result" {
				return append(blocks, contentID{Type: resultType, ToolUseID: result.CallID().String(), Content: failure.Message(), IsError: true}), nil
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
		if resultType == "tool_result" {
			return append(blocks, contentID{Type: resultType, ToolUseID: result.CallID().String(), Content: content}), nil
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
	return nil, canonical.InternalError("Messages received an invalid canonical item kind")
}

func encodeMessagesToolCall(item canonical.CanonicalItem, compiled compiledToolProjection) (contentID, error) {
	call, ok := item.ToolCall()
	if !ok {
		return contentID{}, canonical.InternalError("messages tool-call item is invalid")
	}
	tool := call.Tool()
	_, found := compiled.lowered.FindSource(tool)
	projection, foundProjection := compiled.occurrences[tool]
	if !found || !foundProjection || projection.ProjectCall == nil {
		return contentID{}, canonical.InternalError("Messages tool-call history has no emitted target identity")
	}
	projected, err := projection.ProjectCall(call)
	if err != nil {
		return contentID{}, err
	}
	if projected.Type == "" {
		return contentID{}, canonical.InternalError("Messages tool-call projection returned an invalid carrier")
	}
	return contentID{Type: projected.Type, ID: call.CallID().String(), Name: projected.Name, Input: projected.Input}, nil
}

func messagesTextOnlyContent(parts []canonical.MessagePart, surface string) (string, error) {
	var builder strings.Builder
	for _, part := range parts {
		text, ok := part.Text()
		if !ok {
			return "", canonical.InternalError(surface + " received an invalid canonical content variant")
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
			return nil, canonical.InternalError("Messages received an invalid canonical tool-result part")
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
	*changeLog = compat.AppendUnique(*changeLog, change)
	return nil
}

func encodeMessagesTools(tools []canonical.ToolDeclaration, deferred map[canonical.ToolKey]struct{}, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string) ([]ProviderRequestTool, error) {
	typed, _, err := compileMessagesTools(tools, deferred, names, changeLog, exchangeID, DefaultToolLowering())
	return typed, err
}

// DefaultToolLowering returns official Messages semantics for every slot.
func DefaultToolLowering() ToolLowering {
	function := func(ctx ToolLoweringContext, tool canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
		decl, ok := tool.Function()
		if !ok {
			return ToolProjection{}, nil, canonical.InternalError("Messages Function slot received a non-Function declaration")
		}
		encoded, err := encodeMessagesFunctionTool(tool, decl, ctx.Names)
		if err != nil {
			return ToolProjection{}, nil, err
		}
		return messagesCallableProjection(encoded, "tool_use", "tool_result"), nil, nil
	}
	discovery := func(ctx ToolLoweringContext, tool canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
		decl, ok := tool.Discovery()
		if !ok {
			return ToolProjection{}, nil, canonical.InternalError("Messages Discovery slot received a non-Discovery declaration")
		}
		if decl.Executor() == canonical.DiscoveryExecutorClient {
			schema, err := messagesToolSchema(decl.InputSchema())
			if err != nil {
				return ToolProjection{}, nil, err
			}
			name, err := wire.EncodeToolName(ctx.Names, tool.Key())
			if err != nil {
				return ToolProjection{}, nil, err
			}
			return messagesCallableProjection(ProviderRequestTool{Name: name, Description: decl.Description(), InputSchema: schema}, "tool_use", "tool_result"), nil, nil
		}
		typeName, name, ok := messagesProviderDiscoveryTool(decl)
		if !ok {
			return ToolProjection{}, []compat.Change{compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(tool.Key()))}, nil
		}
		return messagesCallableProjection(ProviderRequestTool{Type: typeName, Name: name}, "server_tool_use", "tool_search_tool_result"), nil, nil
	}
	omit := func(_ ToolLoweringContext, tool canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
		return ToolProjection{}, []compat.Change{compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(tool.Key()))}, nil
	}
	return ToolLowering{
		Function: function, Custom: omit,
		WebSearch: func(_ ToolLoweringContext, tool canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
			return messagesHostedSearchProjection(nil), []compat.Change{compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(tool.Key()))}, nil
		},
		Discovery: discovery,
	}
}

func messagesCallableProjection(fragment ProviderRequestTool, callType, resultType string) ToolProjection {
	targetType := fragment.Type
	if targetType == "" {
		targetType = "tool"
	}
	return ToolProjection{Fragments: []ProviderRequestTool{fragment}, TargetType: targetType, TargetName: fragment.Name,
		ProjectCall: func(call canonical.ToolCallItem) (ToolCallProjection, error) {
			object, ok := call.Input().Object()
			if !ok {
				return ToolCallProjection{}, canonical.BadRequest("messages callable tool calls require object input")
			}
			return ToolCallProjection{Type: callType, Name: fragment.Name, Input: json.RawMessage(object.Bytes())}, nil
		},
		ProjectResult: func(canonical.ToolResultItem) (ToolResultProjection, error) {
			return ToolResultProjection{Type: resultType}, nil
		}}
}

func messagesHostedSearchProjection(fragment *ProviderRequestTool) ToolProjection {
	p := ToolProjection{TargetName: "web_search", ProjectCall: func(call canonical.ToolCallItem) (ToolCallProjection, error) {
		search, ok := call.Input().WebSearch()
		if !ok || search.Action != canonical.WebSearchActionSearch || len(search.Queries) != 1 {
			return ToolCallProjection{}, canonical.InternalError("Messages received an invalid canonical multi-query server-tool item")
		}
		input, err := json.Marshal(map[string]string{"query": search.Queries[0]})
		if err != nil {
			return ToolCallProjection{}, canonical.InternalError("messages web-search call could not be encoded")
		}
		return ToolCallProjection{Type: "server_tool_use", Name: "web_search", Input: input}, nil
	}, ProjectResult: func(canonical.ToolResultItem) (ToolResultProjection, error) {
		return ToolResultProjection{Type: "web_search_tool_result"}, nil
	}}
	if fragment != nil {
		p.Fragments = []ProviderRequestTool{*fragment}
		p.TargetType = fragment.Type
	}
	return p
}

// HostedSearchProjection returns the complete Messages hosted-search
// manifestation selected by a provider WebSearch override.
func HostedSearchProjection(fragment ProviderRequestTool) ToolProjection {
	return messagesHostedSearchProjection(&fragment)
}

// CallableProjection returns a complete Messages callable manifestation for a
// slot that intentionally maps another canonical semantic onto tool_use.
func CallableProjection(fragment ProviderRequestTool) ToolProjection {
	return messagesCallableProjection(fragment, "tool_use", "tool_result")
}

// DefaultLowering returns total official Messages semantics.
func DefaultLowering() Lowering {
	return Lowering{
		Tools: DefaultToolLowering(),
		Reasoning: func(payload map[string]any, reasoning canonical.ReasoningControls, changeLog *[]compat.Change) error {
			return encodeMessagesReasoning(payload, reasoning, false, changeLog)
		},
	}
}

// OmitAdaptiveReasoning is a sparse target override for Messages providers
// whose grammar cannot accept adaptive or budget reasoning controls.
func OmitAdaptiveReasoning(payload map[string]any, reasoning canonical.ReasoningControls, changeLog *[]compat.Change) error {
	return encodeMessagesReasoning(payload, reasoning, true, changeLog)
}

func compileMessagesTools(tools []canonical.ToolDeclaration, deferred map[canonical.ToolKey]struct{}, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string, lowering ToolLowering) ([]ProviderRequestTool, compiledToolProjection, error) {
	if len(tools) == 0 {
		return nil, compiledToolProjection{occurrences: make(map[canonical.ToolKey]ToolProjection)}, nil
	}
	for _, tool := range tools {
		if decl, ok := tool.Function(); ok {
			if strict, specified := decl.Strict().Get(); specified && strict {
				if err := appendMessagesRequestChange(changeLog, exchangeID, canonical.RequestToolsSchemaStrict, compat.Omission); err != nil {
					return nil, compiledToolProjection{}, err
				}
				break
			}
		}
	}
	out := make([]ProviderRequestTool, 0, len(tools))
	compiled := compiledToolProjection{lowered: wire.LoweredToolSet{Records: make([]wire.LoweredToolRecord, 0, len(tools))}, occurrences: make(map[canonical.ToolKey]ToolProjection, len(tools))}
	if !lowering.resolved() {
		return nil, compiledToolProjection{}, canonical.InternalError("Messages tool compilation requires resolved lowering")
	}
	for ordinal, tool := range tools {
		var transformer ToolTransformer
		switch tool.Kind() {
		case canonical.ToolKindFunction:
			transformer = lowering.Function
		case canonical.ToolKindCustom:
			transformer = lowering.Custom
		case canonical.ToolKindWebSearch:
			transformer = lowering.WebSearch
		case canonical.ToolKindDiscovery:
			transformer = lowering.Discovery
		}
		if transformer == nil {
			return nil, compiledToolProjection{}, canonical.InternalError("Messages lowering contains an unresolved tool slot")
		}
		projection, changes, err := transformer(ToolLoweringContext{Ordinal: uint32(ordinal), Names: names}, tool)
		if changeLog != nil {
			*changeLog = append(*changeLog, changes...)
		}
		if err != nil {
			return nil, compiledToolProjection{}, err
		}
		for index := range projection.Fragments {
			_, projection.Fragments[index].DeferLoading = deferred[tool.Key()]
		}
		out = append(out, projection.Fragments...)
		record := wire.LoweredToolRecord{Key: tool.Key(), Kind: tool.Kind(), FragmentCount: len(projection.Fragments), TargetType: projection.TargetType, TargetName: projection.TargetName}
		compiled.lowered.Records = append(compiled.lowered.Records, record)
		compiled.occurrences[tool.Key()] = projection
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
				*changeLog = compat.AppendUnique(*changeLog, compat.NewApproximation(canonical.RequestToolsVisibility, canonical.Occurrence{}))
			}
		}
	}
	return out, compiled, nil
}

func hasEagerProviderDiscovery(tools []canonical.ToolDeclaration) bool {
	for _, tool := range tools {
		if discovery, ok := tool.Discovery(); ok && discovery.Executor() == canonical.DiscoveryExecutorProvider {
			_, _, native := messagesProviderDiscoveryTool(discovery)
			if !native {
				return true
			}
		}
	}
	return false
}

func removeEagerProviderDiscovery(tools []canonical.ToolDeclaration) []canonical.ToolDeclaration {
	projected := make([]canonical.ToolDeclaration, 0, len(tools))
	for _, tool := range tools {
		if discovery, ok := tool.Discovery(); ok && discovery.Executor() == canonical.DiscoveryExecutorProvider {
			_, _, native := messagesProviderDiscoveryTool(discovery)
			if !native {
				continue
			}
		}
		projected = append(projected, tool)
	}
	return projected
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
