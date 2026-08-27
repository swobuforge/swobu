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
	"github.com/swobuforge/swobu/internal/wire"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
)

type EncodeOptions struct {
	Instructions string
}

// CompileOptions contains the occurrence-local target rules used while the
// shared Responses compiler still owns traversal and dependent policy order.
type CompileOptions struct {
	LowerTool                  ToolLoweringRule
	LowerToolPolicy            ToolPolicyLoweringRule
	PrependInstructionsToInput bool
	OmitInclude                bool
	OmitStoreFalse             bool
	ForceArrayInput            bool
	DefaultStore               *bool
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
	Request         canonical.CanonicalRequest
	PreviousHistory *provider.PreviousHistory
	ToolNames       wire.ToolNames
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
	document, err := CompileProviderRequestDocument(input, d, changeLog, exchangeID, options, CompileOptions{})
	if err != nil {
		return carrier.Document{}, err
	}
	return EncodeProviderRequestDocument(document)
}

// CompileProviderRequestDocument lowers one exact target dialect before the
// single serialization boundary.
func CompileProviderRequestDocument(input EncodeInput, d delivery.Delivery, changeLog *[]compat.Change, exchangeID string, options EncodeOptions, compile CompileOptions) (ProviderRequestDocument, error) {
	req := input.Request
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return ProviderRequestDocument{}, canonical.InternalError("Responses received an invalid delivery mode")
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
	responsesRefined := false
	inputRequest := req
	if previous := input.PreviousHistory; previous != nil && previous.Response.Responses != nil {
		if previous.Response.Responses.ProviderResponseID.String() == "" || previous.OmitStart > previous.OmitEnd || uint64(previous.OmitEnd) > uint64(len(items)) {
			return ProviderRequestDocument{}, fmt.Errorf("responses provider encoding received invalid previous-response data")
		}
		projected := make([]canonical.CanonicalItem, 0, len(items)-int(previous.OmitEnd-previous.OmitStart))
		projected = append(projected, items[:previous.OmitStart]...)
		projected = append(projected, items[previous.OmitEnd:]...)
		inputRequest = req.WithItems(projected)
		responsesRefined = true
	}
	payloadInput, err := encodeInput(inputRequest, input.ToolNames, changeLog, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}

	preludeItems := prelude.Items()
	requestVisibility, err := responsesToolVisibilityAt(preludeItems)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	policy, err := req.EffectiveToolPolicy()
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	wireTools, loweredTools, err := compileResponsesTools(requestTools, requestVisibility, input.ToolNames, changeLog, exchangeID, compile.LowerTool, &policy)
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

	var choice any
	if compile.LowerToolPolicy != nil {
		var handled bool
		var changes []compat.Change
		choice, handled, changes, err = compile.LowerToolPolicy(policy, loweredTools, input.ToolNames)
		if changeLog != nil {
			*changeLog = append(*changeLog, changes...)
		}
		if err != nil {
			return ProviderRequestDocument{}, err
		}
		if !handled {
			choice, err = encodeToolChoice(policy, loweredTools, input.ToolNames, changeLog, exchangeID)
		}
	} else {
		choice, err = encodeToolChoice(policy, loweredTools, input.ToolNames, changeLog, exchangeID)
	}
	if err != nil {
		return ProviderRequestDocument{}, err
	}

	payload := map[string]any{"model": req.Model()}
	loweredInstructions := flattenInstructionsForResponses(preludeItems)
	logResponsesEncodeShape(req, tools, loweredInstructions.Text, payloadInput, choice, policy, d)
	if changeLog != nil {
		*changeLog = append(*changeLog, loweredInstructions.Changes...)
	}
	if compile.PrependInstructionsToInput {
		if text := loweredInstructions.Text; text != "" {
			payloadInput, err = prependResponsesInstruction(payloadInput, text)
			if err != nil {
				return ProviderRequestDocument{}, err
			}
		}
	} else {
		if instructions := mergedResponsesInstructions(loweredInstructions.Text, options.Instructions); instructions != "" {
			payload["instructions"] = instructions
		}
	}
	if compile.ForceArrayInput {
		if inputStr, ok := payloadInput.(string); ok {
			payloadInput = []any{map[string]any{"type": "message", "role": "user", "content": inputStr}}
		}
	}
	var store *bool
	if storeValue, specified := req.Store(); specified {
		store = &storeValue
	}
	if compile.OmitStoreFalse && store != nil && !*store {
		store = nil
	}
	if store == nil && compile.DefaultStore != nil {
		store = compile.DefaultStore
	}
	if err := encodeResponsesToolCallBatch(payload, req.ToolCallBatch(), loweredTools.TotalFragments() > 0); err != nil {
		return ProviderRequestDocument{}, err
	}
	if responsesRefined && req.ToolCallBatchSpecified() && req.ToolCallBatch().IsZero() && loweredTools.TotalFragments() > 0 {
		payload["parallel_tool_calls"] = true
	}
	encodeResponsesGenerationControls(payload, req.Controls(), changeLog)
	if err := encodeResponsesReasoning(payload, req.Reasoning(), req.Controls().Effort, changeLog); err != nil {
		return ProviderRequestDocument{}, err
	}
	if !compile.OmitInclude {
		// Request encrypted reasoning state required to preserve official Responses
		// reasoning continuity when Swobu manages conversation history manually.
		payload["include"] = []string{"reasoning.encrypted_content"}
	}
	if text, err := encodeResponsesOutputFormat(req.OutputFormat()); err != nil {
		return ProviderRequestDocument{}, err
	} else if text != nil {
		payload["text"] = text
	} else if responsesRefined && req.OutputFormatSpecified() {
		payload["text"] = &responsesTextDTO{Format: responsesTextFormatDTO{Type: string(canonical.OutputFormatText)}}
	}
	if responsesRefined {
		payload["previous_response_id"] = input.PreviousHistory.Response.Responses.ProviderResponseID
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

func prependResponsesInstruction(input any, instructions string) ([]any, error) {
	instruction := map[string]any{
		"type": "message", "role": "system",
		"content": []map[string]string{{"type": "input_text", "text": instructions}},
	}
	switch value := input.(type) {
	case string:
		return []any{instruction, map[string]any{"type": "message", "role": "user", "content": []map[string]string{{"type": "input_text", "text": value}}}}, nil
	case []any:
		return append([]any{instruction}, value...), nil
	case nil:
		return []any{instruction}, nil
	default:
		return nil, canonical.InternalError("Responses input has an unsupported typed shape")
	}
}

func responsesToolVisibilityAt(items []canonical.CanonicalItem) (canonical.ToolVisibilityRefinements, error) {
	environment, err := canonical.ToolEnvironmentAt(items, len(items))
	if err != nil {
		return canonical.ToolVisibilityRefinements{}, err
	}
	var deferred []canonical.ToolKey
	for _, item := range items {
		if declarations, ok := item.ToolDeclarations(); ok {
			deferred = append(deferred, declarations.Visibility().DeferredKeys()...)
		}
		if result, ok := item.ToolDiscoveryResult(); ok {
			deferred = append(deferred, result.Visibility().DeferredKeys()...)
		}
	}
	tools, err := canonical.NewToolSet(environment.Declarations())
	if err != nil {
		return canonical.ToolVisibilityRefinements{}, err
	}
	return canonical.NewToolVisibilityRefinements(tools, deferred)
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

func encodeInput(req canonical.CanonicalRequest, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string) (any, error) {
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
		return encodeConversation(req, items, environment.Declarations(), names, changeLog, exchangeID)
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
func encodeConversation(request canonical.CanonicalRequest, items []canonical.CanonicalItem, tools []canonical.ToolDeclaration, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string) ([]any, error) {
	encoded := make([]any, 0, len(items))
	pendingWebSearch := make(map[canonical.ToolCallID]int)
	pendingContentCalls := make(map[canonical.ToolCallID]canonical.ToolKind)
	contentResultKinds, err := contentResultKindsByOccurrence(request.Items())
	if err != nil {
		return nil, err
	}
	for itemIndex, current := range items {
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
			wireTools, err := encodeResponsesTools(declarations.Tools().Declarations(), declarations.Visibility(), names, changeLog, exchangeID)
			if err != nil {
				return nil, err
			}
			encoded = append(encoded, map[string]any{"type": "additional_tools", "role": "developer", "tools": wireTools})
		case canonical.ItemKindToolCall:
			call, _ := current.ToolCall()
			tool := call.Tool()
			switch tool.Kind() {
			case canonical.ToolKindFunction:
				name, err := wire.EncodeToolName(names, tool)
				if err != nil {
					return nil, err
				}
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
				name, err := wire.EncodeToolName(names, tool)
				if err != nil {
					return nil, err
				}
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
				return nil, canonical.NotImplemented("Responses cannot project this canonical tool-call kind")
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
			if _, failed := result.Failure(); failed {
				return nil, canonical.NotImplemented("Responses cannot project a typed failed discovery result")
			}
			wireTools, err := encodeResponsesTools(result.Tools().Declarations(), result.Visibility(), names, changeLog, exchangeID)
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
			hasResponsesReplay := false
			if replay, ok := reasoning.Opaque().Responses(); ok {
				hasResponsesReplay = true
				item["encrypted_content"] = replay.EncryptedContent
				// RFC G2 §7.5: replay the paired Responses wire id verbatim when it
				// was preserved. Idless replay stays idless. The id rides only with
				// encrypted content (see OpaqueThinking invariant), so this never
				// revives an id from a non-Responses branch.
				if replay.ItemID != "" {
					item["id"] = replay.ItemID
				}
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
			// Responses reasoning replay requires the summary collection even when
			// the provider returned no readable summaries. Canonical parts retain
			// summary meaning, while the replay branch restores the required empty
			// wire collection after decode/checkpoint materialization.
			if hasResponsesReplay || len(summary) > 0 {
				item["summary"] = summary
			}
			if len(content) > 0 {
				item["content"] = content
			}
			// A canonical reasoning item may contain only an opaque replay branch
			// owned by another protocol. Dropping that child leaves no valid
			// Responses reasoning union member. Erase the whole occurrence instead
			// of serializing the invalid residual {"type":"reasoning"}.
			if len(item) == 1 {
				if err := appendResponsesOccurrenceChange(changeLog, exchangeID, canonical.RequestItemsKind, compat.Omission, canonical.RequestItemOccurrence(uint32(itemIndex))); err != nil {
					return nil, err
				}
				continue
			}
			encoded = append(encoded, item)
		default:
			return nil, canonical.NotImplemented("Responses cannot project this canonical item kind")
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
	if author != canonical.MessageRoleAssistant && len(parts) == 1 {
		if text, ok := parts[0].Text(); ok {
			return text.Text(), nil
		}
	}
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		if text, ok := part.Text(); ok {
			partType := "input_text"
			if author == canonical.MessageRoleAssistant {
				partType = "output_text"
			}
			out = append(out, map[string]any{"type": partType, "text": text.Text()})
			continue
		}
		if part.Kind() == canonical.PartKindImage {
			if author != canonical.MessageRoleUser {
				return nil, canonical.NotImplemented("Responses cannot project image input outside user messages")
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
		return nil, canonical.NotImplemented("Responses cannot project this canonical content kind")
	}
	return out, nil
}

func responsesTextOnlyContent(parts []canonical.MessagePart, surface string) (string, error) {
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
			return nil, canonical.NotImplemented("Responses cannot project this canonical tool-result content kind")
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

var reservedResponsesSemanticFields = map[string]struct{}{
	"model": {}, "input": {}, "instructions": {}, "tools": {}, "tool_choice": {},
	"parallel_tool_calls": {}, "stream": {}, "temperature": {}, "top_p": {},
	"max_output_tokens": {}, "stop": {}, "response_format": {}, "text": {},
	"output_format": {}, "include": {}, "store": {}, "previous_response_id": {},
	"reasoning_effort": {}, "reasoning": {}, "conversation": {}, "metadata": {}, "user": {},
}

// ApplyAttemptDecoration mutates the Responses request payload with non-semantic
// provider attempt decoration fields, rejecting collisions with standard Responses semantics.
func ApplyAttemptDecoration(payload map[string]any, fields map[string]any) error {
	for k, v := range fields {
		trimmed := strings.ToLower(strings.TrimSpace(k))
		if _, exists := payload[k]; exists {
			return canonical.InternalError(fmt.Sprintf("attempt decoration illegally mutated semantic field %q", k))
		}
		if _, reserved := reservedResponsesSemanticFields[trimmed]; reserved {
			return canonical.InternalError(fmt.Sprintf("attempt decoration illegally mutated semantic field %q", k))
		}
		payload[k] = v
	}
	return nil
}
