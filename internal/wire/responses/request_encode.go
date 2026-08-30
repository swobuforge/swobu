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
	ToolLowering               ToolLowering
	HistoryMessageRole         HistoryMessageRoleTransformer
	PrependInstructionsToInput bool
	OmitInclude                bool
	OmitMaxOutputTokens        bool
	OmitStoreFalse             bool
	ForceArrayInput            bool
	DefaultStore               *bool
	OmitParallelToolCallsFalse func() bool
	AcceptsReasoningEffortMax  func() bool
	AcceptsReasoningDisabled   func() bool
	AcceptsFunctionOutputArray func() bool
}

// HistoryMessageRoleTransformer lowers one history message role at its exact
// occurrence. It may not reorder or rewrite message content.
type HistoryMessageRoleTransformer func(index int, role canonical.MessageRole) (canonical.MessageRole, []compat.Change, error)

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
	document, err := CompileProviderRequestDocument(input, d, changeLog, exchangeID, options, CompileOptions{ToolLowering: DefaultToolLowering()})
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
	preludeItems := prelude.Items()
	fullVisibility, err := responsesToolVisibilityAt(items)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	policy, err := req.EffectiveToolPolicy()
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	toolProjection, err := compileResponsesToolProjection(tools, fullVisibility, input.ToolNames, changeLog, exchangeID, compile.ToolLowering)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	wireTools := toolProjection.fragmentsFor(requestTools)
	payloadInput, err := encodeInput(inputRequest, req.Items(), input.ToolNames, compile.HistoryMessageRole, compile.AcceptsFunctionOutputArray, changeLog, exchangeID, &toolProjection)
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

	choice, err := encodeToolChoice(policy, toolProjection, input.ToolNames, changeLog, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}

	payload := map[string]any{"model": req.Model()}
	loweredInstructions := flattenInstructionsForResponses(preludeItems)
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
	// Shape diagnostics run only after every input transformation so encoded
	// fields describe the document handed to serialization, not canonical or
	// intermediate state.
	logResponsesEncodeShape(req, tools, loweredInstructions.Text, payloadInput, choice, policy, d)
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
	if err := encodeResponsesToolCallBatch(payload, req.ToolCallBatch(), toolProjection.lowered.TotalFragments() > 0, compile.OmitParallelToolCallsFalse, changeLog); err != nil {
		return ProviderRequestDocument{}, err
	}
	if responsesRefined && req.ToolCallBatchSpecified() && req.ToolCallBatch().IsZero() && toolProjection.lowered.TotalFragments() > 0 {
		payload["parallel_tool_calls"] = true
	}
	encodeResponsesGenerationControls(payload, req.Controls(), compile.OmitMaxOutputTokens, changeLog)
	if err := encodeResponsesReasoning(payload, req.Reasoning(), req.Controls().Effort, compile.AcceptsReasoningEffortMax, compile.AcceptsReasoningDisabled, changeLog); err != nil {
		return ProviderRequestDocument{}, err
	}
	if !compile.OmitInclude {
		// Request encrypted reasoning state required to preserve official Responses
		// reasoning continuity when Swobu manages conversation history manually.
		include := []string{"reasoning.encrypted_content"}
		if search, ok := toolProjection.occurrences[canonical.WebSearchToolKey()]; ok && search.SupportsWebSearchSourceInclude {
			include = append(include, "web_search_call.action.sources")
		}
		payload["include"] = include
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
		"encoded_item_count", responsesEncodedItemCount(input),
		"thread_tail_role", responsesTailRole(thread),
		"encoded_tail_role", responsesEncodedTailRole(input),
		"input_type", inputType,
		"tool_count", len(tools),
		"function_tool_count", responsesToolKindCount(tools, canonical.ToolTypeFunction),
		"custom_tool_count", responsesToolKindCount(tools, canonical.ToolTypeCustom),
		"tool_policy", strings.TrimSpace(string(policy.Mode)), // swobu:io-string source=domain
		"tool_choice_shape", responsesWireToolChoiceShape(choice),
		"parallel_tool_calls", strings.TrimSpace(string(req.ToolCallBatch().Mode)), // swobu:io-string source=domain
	)
}

func responsesEncodedTailRole(input any) string {
	switch value := input.(type) {
	case string:
		return "user"
	case []any:
		if len(value) == 0 {
			return ""
		}
		switch item := value[len(value)-1].(type) {
		case inputMessageItem:
			return strings.TrimSpace(item.Role) // swobu:io-string source=provider-wire
		case functionCallItem, customToolCallItem:
			return "assistant"
		case toolCallOutputItem:
			return "tool"
		case map[string]any:
			if role, ok := item["role"].(string); ok {
				return strings.TrimSpace(role) // swobu:io-string source=provider-wire
			}
			kind, _ := item["type"].(string)
			switch strings.TrimSpace(kind) { // swobu:io-string source=provider-wire
			case "function_call_output", "custom_tool_call_output":
				return "tool"
			case "function_call", "custom_tool_call":
				return "assistant"
			}
		}
	}
	return ""
}

func responsesEncodedItemCount(input any) int {
	switch value := input.(type) {
	case string:
		return 1
	case []any:
		return len(value)
	default:
		return 0
	}
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

func encodeInput(req canonical.CanonicalRequest, correlationItems []canonical.CanonicalItem, names wire.ToolNames, historyMessageRole HistoryMessageRoleTransformer, acceptsFunctionOutputArray func() bool, changeLog *[]compat.Change, exchangeID string, projection *responsesToolProjection) (any, error) {
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
		return encodeConversation(items, correlationItems, environment.Declarations(), names, historyMessageRole, acceptsFunctionOutputArray, changeLog, exchangeID, projection)
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
func encodeConversation(items, correlationItems []canonical.CanonicalItem, tools []canonical.ToolDeclaration, names wire.ToolNames, historyMessageRole HistoryMessageRoleTransformer, acceptsFunctionOutputArray func() bool, changeLog *[]compat.Change, exchangeID string, projection *responsesToolProjection) ([]any, error) {
	encoded := make([]any, 0, len(items))
	pendingWebSearch := make(map[canonical.ToolCallID]int)
	projectedEffects, err := projectedResponsesEffects(correlationItems, names, projection)
	if err != nil {
		return nil, err
	}
	callRecords := projectedEffects.callQueues()
	resultRecords := projectedEffects.resultQueues()
	for itemIndex, current := range items {
		switch current.Kind() {
		case canonical.ItemKindMessage:
			message, _ := current.Message()
			if message.Scope() == canonical.ContextScopeRequest {
				continue
			}
			role := message.Role()
			if historyMessageRole != nil {
				var roleChanges []compat.Change
				role, roleChanges, err = historyMessageRole(itemIndex, role)
				if err != nil {
					return nil, err
				}
				if changeLog != nil {
					*changeLog = append(*changeLog, roleChanges...)
				}
			}
			content, err := encodeResponsesMessageContent(role, message.Content())
			if err != nil {
				return nil, err
			}
			item := inputMessageItem{
				Type:    "message",
				Role:    string(role),
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
			wireTools := projection.fragmentsFor(declarations.Tools().Declarations())
			encoded = append(encoded, map[string]any{"type": "additional_tools", "role": "developer", "tools": wireTools})
		case canonical.ItemKindToolCall:
			call, _ := current.ToolCall()
			occurrence, found := popProjectedOccurrence(callRecords, call.CallID())
			if !found {
				return nil, canonical.InternalError("Responses tool-call history lost emitted declaration identity")
			}
			if occurrence.ProjectCall == nil {
				if err := appendResponsesOccurrenceChange(changeLog, exchangeID, canonical.RequestItemsKind, compat.Omission, canonical.RequestItemOccurrence(uint32(itemIndex))); err != nil {
					return nil, err
				}
				continue
			}
			projectedCall, err := occurrence.ProjectCall(call)
			if err != nil {
				return nil, err
			}
			switch value := projectedCall.(type) {
			case functionCallItem, customToolCallItem:
				encoded = append(encoded, value)
			case webSearchCallProjection:
				if _, exists := pendingWebSearch[call.CallID()]; exists {
					return nil, canonical.BadRequest("responses web-search history contains a duplicate unresolved call")
				}
				pendingWebSearch[call.CallID()] = len(encoded)
				encoded = append(encoded, value.item)
			case toolSearchCallProjection:
				encoded = append(encoded, map[string]any{"type": "tool_search_call", "call_id": value.callID, "execution": value.execution, "arguments": value.arguments})
			default:
				return nil, canonical.InternalError("Responses projection emitted an unsupported history call carrier")
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
			occurrence, found := popProjectedOccurrence(resultRecords, result.CallID())
			if !found {
				return nil, canonical.InternalError("Responses tool-result history lost emitted declaration identity")
			}
			if occurrence.ProjectResult == nil {
				if err := appendResponsesOccurrenceChange(changeLog, exchangeID, canonical.RequestItemsKind, compat.Omission, canonical.RequestItemOccurrence(uint32(itemIndex))); err != nil {
					return nil, err
				}
				continue
			}
			projectedResult, err := occurrence.ProjectResult(result)
			if err != nil {
				return nil, err
			}
			resultType := projectedResult.Type
			if resultType == "" {
				return nil, canonical.InternalError("Responses projection emitted an invalid history result carrier")
			}
			if result.IsError() {
				if err := appendResponsesRequestChange(changeLog, exchangeID, canonical.RequestItemsToolResultIsError, compat.Approximation); err != nil {
					return nil, err
				}
			}
			var outputArrayFact func() bool
			if resultType == "function_call_output" {
				outputArrayFact = acceptsFunctionOutputArray
			} else {
				outputArrayFact = func() bool { return false }
			}
			content, rehomed, err := encodeResponsesToolResultContent(result.Content(), outputArrayFact, changeLog, exchangeID)
			if err != nil {
				return nil, err
			}
			item := toolCallOutputItem{
				Type:   resultType,
				CallID: result.CallID().String(),
				Output: content,
			}
			encoded = append(encoded, item)
			encoded = append(encoded, rehomed...)
		case canonical.ItemKindToolDiscoveryResult:
			result, _ := current.ToolDiscoveryResult()
			occurrence, found := popProjectedOccurrence(resultRecords, result.CallID())
			if !found {
				if err := appendResponsesOccurrenceChange(changeLog, exchangeID, canonical.RequestItemsKind, compat.Omission, canonical.RequestItemOccurrence(uint32(itemIndex))); err != nil {
					return nil, err
				}
				continue
			}
			if occurrence.ProjectResult == nil {
				if err := appendResponsesOccurrenceChange(changeLog, exchangeID, canonical.RequestItemsKind, compat.Omission, canonical.RequestItemOccurrence(uint32(itemIndex))); err != nil {
					return nil, err
				}
				continue
			}
			projectedResult, err := occurrence.ProjectResult(canonical.ToolResultItem{})
			if err != nil {
				return nil, err
			}
			resultType := projectedResult.Type
			if resultType == "" {
				return nil, canonical.InternalError("Responses discovery result projection is invalid")
			}
			if _, failed := result.Failure(); failed {
				return nil, canonical.InternalError("Responses received an invalid canonical failed-discovery item")
			}
			wireTools := projection.fragmentsFor(result.Tools().Declarations())
			execution := "client"
			if result.Executor() == canonical.DiscoveryExecutorProvider {
				execution = "server"
			}
			var wireCallID any = result.CallID().String()
			if result.ResponsesCallIDNull() {
				wireCallID = nil
			}
			encoded = append(encoded, map[string]any{"type": resultType, "call_id": wireCallID, "status": "completed", "execution": execution, "tools": wireTools})
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
			return nil, canonical.InternalError("Responses received an invalid canonical item kind")
		}
	}
	return encoded, nil
}

type projectedResponsesEffect struct {
	callID     canonical.ToolCallID
	projection ToolProjection
	result     bool
}

type projectedResponsesEffectSet []projectedResponsesEffect

func (s projectedResponsesEffectSet) callQueues() map[canonical.ToolCallID][]ToolProjection {
	queues := make(map[canonical.ToolCallID][]ToolProjection)
	for _, effect := range s {
		queues[effect.callID] = append(queues[effect.callID], effect.projection)
	}
	return queues
}

func (s projectedResponsesEffectSet) resultQueues() map[canonical.ToolCallID][]ToolProjection {
	queues := make(map[canonical.ToolCallID][]ToolProjection)
	for _, effect := range s {
		if effect.result {
			queues[effect.callID] = append(queues[effect.callID], effect.projection)
		}
	}
	return queues
}

func popProjectedOccurrence(queues map[canonical.ToolCallID][]ToolProjection, callID canonical.ToolCallID) (ToolProjection, bool) {
	occurrences := queues[callID]
	if len(occurrences) == 0 {
		return ToolProjection{}, false
	}
	queues[callID] = occurrences[1:]
	return occurrences[0], true
}

func projectedResponsesEffects(items []canonical.CanonicalItem, names wire.ToolNames, projection *responsesToolProjection) (projectedResponsesEffectSet, error) {
	effects, err := canonical.MatchToolEffects(items)
	if err != nil {
		standaloneDiscoveryResults := len(items) > 0
		for _, item := range items {
			if _, ok := item.ToolDiscoveryResult(); !ok {
				standaloneDiscoveryResults = false
				break
			}
		}
		if standaloneDiscoveryResults {
			return nil, nil
		}
		return nil, canonical.BadRequest("responses history has invalid tool-effect correlation: " + err.Error())
	}
	projected := make(projectedResponsesEffectSet, 0, len(effects))
	for _, effect := range effects {
		call, _ := items[effect.CallIndex].ToolCall()
		occurrence, err := projection.historicalProjection(call, names)
		if err != nil {
			return nil, err
		}
		projected = append(projected, projectedResponsesEffect{callID: effect.CallID, projection: occurrence, result: effect.ResultIndex >= 0})
	}
	return projected, nil
}

func appendResponsesRequestChange(changeLog *[]compat.Change, exchangeID string, feature canonical.CapabilityPath, outcome compat.Kind) error {
	if changeLog == nil {
		return nil
	}
	change := compat.Change{Capability: feature, Kind: outcome}
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
				return nil, canonical.InternalError("Responses received an invalid canonical image variant")
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
		return nil, canonical.InternalError("Responses received an invalid canonical content variant")
	}
	return out, nil
}

func responsesTextOnlyContent(parts []canonical.MessagePart, surface string) (string, error) {
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

func encodeResponsesToolResultContent(parts []canonical.ToolResultPart, acceptsArray func() bool, changeLog *[]compat.Change, exchangeID string) (any, []any, error) {
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
	if acceptsArray != nil && !acceptsArray() {
		if textOnly {
			return text.String(), nil, nil
		}
		images := make([]any, 0, len(parts))
		for _, part := range parts {
			image, ok := part.Image()
			if !ok {
				continue
			}
			rawURL, detail, err := openaiwire.EncodeOpenAIImageURL(image)
			if err != nil {
				return nil, nil, canonical.InternalError("canonical image source is invalid")
			}
			wireImage := map[string]any{"type": "input_image", "image_url": rawURL}
			if detail != "" {
				wireImage["detail"] = string(detail)
			}
			images = append(images, wireImage)
		}
		if err := appendResponsesRequestChange(changeLog, exchangeID, canonical.RequestItemsToolResultImage, compat.Approximation); err != nil {
			return nil, nil, err
		}
		return text.String(), []any{inputMessageItem{Type: "message", Role: "user", Content: images}}, nil
	}
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		if text, ok := part.Text(); ok {
			out = append(out, map[string]any{"type": "input_text", "text": text.Text()})
			continue
		}
		image, ok := part.Image()
		if !ok {
			return nil, nil, canonical.InternalError("Responses received an invalid canonical tool-result part")
		}
		rawURL, detail, err := openaiwire.EncodeOpenAIImageURL(image)
		if err != nil {
			return nil, nil, canonical.InternalError("canonical image source is invalid")
		}
		wireImage := map[string]any{"type": "input_image", "image_url": rawURL}
		if detail != "" {
			wireImage["detail"] = string(detail)
		}
		out = append(out, wireImage)
	}
	return out, nil, nil
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
