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
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire"
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

type toolCallOutputItem struct {
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
	Request           canonical.CanonicalRequest
	ResponsesPrevious *provider.ResponsesPrevious
	ToolNames         wire.ToolNames
	Access            mcp.Access
}

// ProviderRequestDocument is the typed official Responses request before an
// exact provider owns any positive typed adaptation.
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
func EncodeCarrierWithChanges(input EncodeInput, d delivery.Delivery, changeLog *[]compat.Change, exchangeID string, options EncodeOptions) (carrier.Document, error) {
	document, err := LowerProviderRequestDocument(input, d, changeLog, exchangeID, options)
	if err != nil {
		return carrier.Document{}, err
	}
	return EncodeProviderRequestDocument(document)
}

// LowerProviderRequestDocument produces a typed Responses document without
// crossing the JSON boundary.
func LowerProviderRequestDocument(input EncodeInput, d delivery.Delivery, changeLog *[]compat.Change, exchangeID string, options EncodeOptions) (ProviderRequestDocument, error) {
	req := input.Request
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return ProviderRequestDocument{}, provider.NewIncompatibleTarget("Responses target cannot represent the requested canonical delivery mode")
	}

	items := req.Items()
	if wire.HasDeferredResponsesTools(items) {
		if err := appendResponsesRequestChange(changeLog, exchangeID, canonical.RequestToolsVisibility, compat.Approximation); err != nil {
			return ProviderRequestDocument{}, err
		}
	}
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
	responsesRefined := input.ResponsesPrevious != nil
	inputRequest := req
	if previous := input.ResponsesPrevious; previous != nil {
		if previous.ProviderResponseID.String() == "" || previous.OmitStart > previous.OmitEnd || uint64(previous.OmitEnd) > uint64(len(items)) {
			return ProviderRequestDocument{}, fmt.Errorf("responses provider encoding received invalid previous-response data")
		}
		projected := make([]canonical.CanonicalItem, 0, len(items)-int(previous.OmitEnd-previous.OmitStart))
		projected = append(projected, items[:previous.OmitStart]...)
		projected = append(projected, items[previous.OmitEnd:]...)
		inputRequest = req.WithItems(projected)
	}
	payloadInput, err := encodeInput(inputRequest, input.ToolNames, input.Access, changeLog, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	policy, err := req.EffectiveToolPolicy()
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	flatTools, err := wire.PrepareFlatToolSet(tools, func(tool canonical.ToolDeclaration) (string, error) {
		return responsesFlatToolIdentity(tool, input.ToolNames)
	})
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	choiceTools := flatTools.Declarations
	if flatTools.OmittedMCP > 0 {
		if err := wire.ValidateFlatToolPolicy(policy, choiceTools); err != nil {
			return ProviderRequestDocument{}, err
		}
	}
	choice, err := encodeToolChoice(policy, choiceTools, input.ToolNames, changeLog, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}

	payload := map[string]any{"model": req.Model()}
	preludeItems := prelude.Items()
	loweredInstructions := flattenInstructionsForResponses(preludeItems)
	logResponsesEncodeShape(req, tools, loweredInstructions.Text, payloadInput, choice, policy, d)
	if changeLog != nil {
		*changeLog = append(*changeLog, loweredInstructions.Changes...)
	}
	if instructions := mergedResponsesInstructions(loweredInstructions.Text, options.Instructions); instructions != "" {
		payload["instructions"] = instructions
	}
	var store *bool
	if storeValue, specified := req.Responses().Store(); specified {
		store = &storeValue
	}
	wireTools, err := encodeResponsesTools(requestTools, input.ToolNames, input.Access, changeLog, exchangeID)
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
	if err := encodeResponsesReasoning(payload, req.Reasoning(), req.Controls().Effort, changeLog); err != nil {
		return ProviderRequestDocument{}, err
	}
	// Request encrypted reasoning state required to preserve official Responses
	// reasoning continuity when Swobu manages conversation history manually.
	payload["include"] = []string{"reasoning.encrypted_content"}
	if text, err := encodeResponsesOutputFormat(req.OutputFormat()); err != nil {
		return ProviderRequestDocument{}, err
	} else if text != nil {
		payload["text"] = text
	} else if responsesRefined && req.OutputFormatSpecified() {
		payload["text"] = &responsesTextDTO{Format: responsesTextFormatDTO{Type: string(canonical.OutputFormatText)}}
	}
	if responsesRefined {
		payload["previous_response_id"] = input.ResponsesPrevious.ProviderResponseID
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
		Store:          store,
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
		"tool_choice_shape", responsesWireToolChoiceShape(choice),
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

func responsesWireToolChoiceShape(choice any) string {
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

func encodeInput(req canonical.CanonicalRequest, names wire.ToolNames, access mcp.Access, changeLog *[]compat.Change, exchangeID string) (any, error) {
	items := req.Items()
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
		return encodeConversation(req, items, environment.Declarations(), names, access, changeLog, exchangeID)
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
func encodeConversation(request canonical.CanonicalRequest, items []canonical.CanonicalItem, tools []canonical.ToolDeclaration, names wire.ToolNames, access mcp.Access, changeLog *[]compat.Change, exchangeID string) ([]any, error) {
	encoded := make([]any, 0, len(items))
	pendingWebSearch := make(map[canonical.ToolCallID]int)
	pendingContentCalls := make(map[canonical.ToolCallID]canonical.ToolKind)
	contentResultKinds, err := contentResultKindsByOccurrence(request.Items())
	if err != nil {
		return nil, err
	}
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
			wireTools, err := encodeResponsesTools(declarations.Tools().Declarations(), names, access, changeLog, exchangeID)
			if err != nil {
				return nil, err
			}
			encoded = append(encoded, map[string]any{"type": "additional_tools", "role": "developer", "tools": wireTools})
		case canonical.ItemKindToolCall:
			call, _ := current.ToolCall()
			tool := call.Tool()
			name, err := wire.EncodeToolName(names, tool)
			if err != nil {
				return nil, err
			}
			switch tool.Kind() {
			case canonical.ToolKindFunction:
				if _, exists := pendingContentCalls[call.CallID()]; exists {
					return nil, canonical.BadRequest("responses history contains a duplicate unresolved tool call")
				}
				object, ok := call.Input().Object()
				if !ok {
					return nil, canonical.BadRequest("responses function calls require object input")
				}
				item := functionCallItem{Type: "function_call", CallID: call.CallID().String(), Name: name, Arguments: object.String()}
				encoded = append(encoded, item)
				pendingContentCalls[call.CallID()] = canonical.ToolKindFunction
			case canonical.ToolKindCustom:
				if _, exists := pendingContentCalls[call.CallID()]; exists {
					return nil, canonical.BadRequest("responses history contains a duplicate unresolved tool call")
				}
				text, ok := call.Input().Text()
				if !ok {
					return nil, canonical.BadRequest("responses custom tool calls require text input")
				}
				item := customToolCallItem{Type: "custom_tool_call", CallID: call.CallID().String(), Name: name, Input: text}
				encoded = append(encoded, item)
				pendingContentCalls[call.CallID()] = canonical.ToolKindCustom
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
				searchItem := responsesWireOutputItemDTO{
					Type:   "web_search_call",
					Status: "in_progress",
					Action: action,
				}
				// A Responses item id is provider-owned identity, not canonical
				// correlation. It is emitted only when an exact refinement was
				// preserved (client- or provider-supplied ws id); an idless replay
				// (e.g. Codex under store:false) re-encodes with no id rather than
				// minting the call's correlation token into item.id.
				if refinement, ok := call.ResponsesWebSearch(); ok {
					searchItem.ID = refinement.ItemID().String()
				}
				encoded = append(encoded, searchItem)
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
				var wireCallID any = call.CallID().String()
				if call.ResponsesCallIDNull() {
					wireCallID = nil
				}
				encoded = append(encoded, map[string]any{"type": "tool_search_call", "call_id": wireCallID, "execution": execution, "arguments": json.RawMessage(object.String())})
			default:
				return nil, provider.IncompatibleCapability(canonical.RequestItemsToolCallTool, canonical.CallOccurrence(call.CallID()), "Responses cannot represent this canonical tool-call kind")
			}
		case canonical.ItemKindToolResult:
			result, _ := current.ToolResult()
			if _, ok := result.WebSearch(); ok {
				index, found := pendingWebSearch[result.CallID()]
				if !found {
					return nil, canonical.BadRequest("responses web-search result has no prior call")
				}
				item, ok := encoded[index].(responsesWireOutputItemDTO)
				if !ok {
					return nil, canonical.InternalError("responses web-search request fold is invalid")
				}
				// Compatibility replay carries executable call state only. Sources
				// remain canonical-complete for client responses and citations.
				item.Status = "completed"
				encoded[index] = item
				delete(pendingWebSearch, result.CallID())
				continue
			}
			if result.IsError() {
				if err := appendResponsesRequestChange(changeLog, exchangeID, canonical.RequestItemsToolResultIsError, compat.Approximation); err != nil {
					return nil, err
				}
			}
			content, err := encodeResponsesToolResultContent(result.Content(), changeLog, exchangeID)
			if err != nil {
				return nil, err
			}
			outputType := "function_call_output"
			callKind, found := pendingContentCalls[result.CallID()]
			if !found {
				occurrences := contentResultKinds[result.CallID()]
				if len(occurrences) == 0 {
					// A result-only request contribution has no canonical call
					// kind. Preserve the Responses portable default.
					callKind = canonical.ToolKindFunction
				} else {
					callKind = occurrences[0]
					contentResultKinds[result.CallID()] = occurrences[1:]
				}
			} else {
				occurrences := contentResultKinds[result.CallID()]
				if len(occurrences) == 0 || occurrences[0] != callKind {
					return nil, canonical.InternalError("responses tool-result occurrence pairing is inconsistent")
				}
				contentResultKinds[result.CallID()] = occurrences[1:]
			}
			if callKind == canonical.ToolKindCustom {
				outputType = "custom_tool_call_output"
			}
			delete(pendingContentCalls, result.CallID())
			item := toolCallOutputItem{
				Type:   outputType,
				CallID: result.CallID().String(),
				Output: content,
			}
			encoded = append(encoded, item)
		case canonical.ItemKindToolDiscoveryResult:
			result, _ := current.ToolDiscoveryResult()
			wireTools, err := encodeResponsesTools(result.Tools().Declarations(), names, access, changeLog, exchangeID)
			if err != nil {
				return nil, err
			}
			execution := "client"
			if result.Executor() == canonical.DiscoveryExecutorProvider {
				execution = "server"
			}
			var wireCallID any = result.CallID().String()
			if result.ResponsesCallIDNull() {
				wireCallID = nil
			}
			encoded = append(encoded, map[string]any{"type": "tool_search_output", "call_id": wireCallID, "status": "completed", "execution": execution, "tools": wireTools})
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
			return nil, provider.IncompatibleCapability(canonical.RequestItemsKind, canonical.Occurrence{}, "Responses cannot represent this canonical item kind")
		}
	}
	return encoded, nil
}

func contentResultKindsByOccurrence(items []canonical.CanonicalItem) (map[canonical.ToolCallID][]canonical.ToolKind, error) {
	kinds := make(map[canonical.ToolCallID][]canonical.ToolKind)
	var matcher canonical.ToolEffectMatcher
	for index, item := range items {
		if call, ok := item.ToolCall(); ok {
			if call.Tool().Kind() != canonical.ToolKindFunction && call.Tool().Kind() != canonical.ToolKindCustom {
				continue
			}
		} else if result, ok := item.ToolResult(); !ok {
			continue
		} else if _, webSearch := result.WebSearch(); webSearch {
			continue
		}
		completed, err := matcher.Accept(index, item)
		if err != nil {
			// A native continuation contribution may contain only the result;
			// its call kind remains behind the OpenAI Responses provider response
			// ID used as previous_response_id. The encoder
			// preserves Responses' portable function-result default below.
			if _, resultOnly := item.ToolResult(); resultOnly {
				continue
			}
			return nil, canonical.BadRequest("responses history has invalid tool-effect correlation: " + err.Error())
		}
		if completed != nil {
			kinds[completed.CallID] = append(kinds[completed.CallID], completed.Kind)
		}
	}
	return kinds, nil
}

func appendResponsesRequestChange(changeLog *[]compat.Change, exchangeID string, feature canonical.CapabilityPath, outcome compat.Kind) error {
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
				return nil, provider.IncompatibleCapability(canonical.RequestItemsMessageImage, canonical.Occurrence{}, "Responses accepts canonical image input only in user messages")
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
		return nil, provider.IncompatibleCapability(canonical.RequestItemsKind, canonical.Occurrence{}, "Responses cannot represent this canonical content kind")
	}
	return out, nil
}

func responsesTextOnlyContent(parts []canonical.MessagePart, surface string) (string, error) {
	var builder strings.Builder
	for _, part := range parts {
		text, ok := part.Text()
		if !ok {
			return "", provider.IncompatibleCapability(canonical.RequestItemsMessageImage, canonical.Occurrence{}, surface+" cannot represent this canonical content kind")
		}
		builder.WriteString(text.Text())
	}
	return builder.String(), nil
}

func encodeResponsesToolResultContent(parts []canonical.ToolResultPart, changeLog *[]compat.Change, exchangeID string) (any, error) {
	if len(parts) == 1 {
		if text, ok := parts[0].Text(); ok {
			return text.Text(), nil
		}
	}
	var text strings.Builder
	textOnly := true
	for _, part := range parts {
		value, ok := part.Text()
		if !ok {
			textOnly = false
			break
		}
		text.WriteString(value.Text())
	}
	if textOnly {
		if err := appendResponsesRequestChange(changeLog, exchangeID, canonical.RequestItemsToolResultContent, compat.Approximation); err != nil {
			return nil, err
		}
		return text.String(), nil
	}
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		if text, ok := part.Text(); ok {
			out = append(out, map[string]any{"type": "input_text", "text": text.Text()})
			continue
		}
		image, ok := part.Image()
		if !ok {
			return nil, provider.IncompatibleCapability(canonical.RequestItemsToolResultContent, canonical.Occurrence{}, "Responses tool results cannot represent this canonical content kind")
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
