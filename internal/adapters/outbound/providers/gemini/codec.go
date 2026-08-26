package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
	"github.com/swobuforge/swobu/internal/wire/reasoningprojection"
)

// codec owns the provider-private native Gemini Interactions grammar. Controls,
// images, reasoning, ordinary functions, and Google Search remain here rather
// than creating a shared Interactions wire package.
type codec struct{}

func (codec) Encode(request provider.Request) (carrier.Document, []compat.Change, error) {
	if request.Delivery != delivery.StreamingDelivery(delivery.FramingSSE) {
		return carrier.Document{}, nil, provider.NewIncompatibleTarget("Gemini target requires SSE streaming delivery")
	}
	encoded, changes, err := encodeTextRequest(request)
	if err != nil {
		return carrier.Document{}, changes, err
	}
	raw, err := json.Marshal(encoded)
	if err != nil {
		return carrier.Document{}, changes, canonical.InternalError("Gemini Interactions request could not be encoded")
	}
	return carrier.NewDocument(protocolkind.Interactions, "application/json", nil, raw, carrier.Meta{}), changes, nil
}

func (codec) Decode(_ context.Context, request provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	stream, ok := ingress.(provider.StreamIngress)
	if !ok {
		return provider.DecodedResponse{}, canonical.InternalError("Gemini Interactions requires an SSE response stream")
	}
	if err := core.ValidateResponseSSEByteStream(stream.Stream); err != nil {
		carrierErr := canonical.InternalError("Gemini Interactions stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_invariant": err.Error()}
		return provider.DecodedResponse{Stream: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	reader := newInteractionsStream(request.Canonical, request.ToolNames, stream.Stream, request.ExchangeID)
	return provider.DecodedResponse{Stream: reader, ProgressiveChanges: reader.Changes}, nil
}

type interactionRequest struct {
	Model                 string                       `json:"model"`
	Input                 []interactionInputStep       `json:"input"`
	SystemInstruction     string                       `json:"system_instruction,omitempty"`
	PreviousInteractionID string                       `json:"previous_interaction_id,omitempty"`
	Store                 *bool                        `json:"store,omitempty"`
	Stream                bool                         `json:"stream"`
	GenerationConfig      *interactionGenerationConfig `json:"generation_config,omitempty"`
	ResponseFormat        *interactionResponseFormat   `json:"response_format,omitempty"`
	Tools                 []interactionTool            `json:"tools,omitempty"`
}

type interactionInputStep struct {
	raw       json.RawMessage
	Type      string               `json:"type"`
	ID        string               `json:"id,omitempty"`
	Name      string               `json:"name,omitempty"`
	Arguments json.RawMessage      `json:"arguments,omitempty"`
	Summary   []interactionContent `json:"summary,omitempty"`
	Signature string               `json:"signature,omitempty"`
	Content   []interactionContent `json:"content,omitempty"`
	CallID    string               `json:"call_id,omitempty"`
	Result    []interactionContent `json:"result,omitempty"`
	IsError   *bool                `json:"is_error,omitempty"`
}

func (step interactionInputStep) MarshalJSON() ([]byte, error) {
	if len(step.raw) > 0 {
		return append([]byte(nil), step.raw...), nil
	}
	type plain interactionInputStep
	return json.Marshal(plain(step))
}

type interactionContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	URI      string `json:"uri,omitempty"`
}

// interactionTextContent remains the narrow text-only test spelling from the
// initial text slice; the private request grammar now admits image content too.
type interactionTextContent = interactionContent

type interactionGenerationConfig struct {
	MaxOutputTokens   *int                   `json:"max_output_tokens,omitempty"`
	StopSequences     []string               `json:"stop_sequences,omitempty"`
	Temperature       *float64               `json:"temperature,omitempty"`
	TopP              *float64               `json:"top_p,omitempty"`
	ThinkingLevel     string                 `json:"thinking_level,omitempty"`
	ThinkingSummaries string                 `json:"thinking_summaries,omitempty"`
	ToolChoice        *interactionToolChoice `json:"tool_choice,omitempty"`
}

type interactionToolChoice struct {
	Mode         string                   `json:"-"`
	AllowedTools *interactionAllowedTools `json:"allowed_tools,omitempty"`
}

func (choice *interactionToolChoice) UnmarshalJSON(raw []byte) error {
	var mode string
	if err := json.Unmarshal(raw, &mode); err == nil {
		choice.Mode = mode
		choice.AllowedTools = nil
		return nil
	}
	var object struct {
		AllowedTools *interactionAllowedTools `json:"allowed_tools"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return err
	}
	choice.Mode = ""
	choice.AllowedTools = object.AllowedTools
	return nil
}

type interactionAllowedTools struct {
	Mode  string   `json:"mode"`
	Tools []string `json:"tools"`
}

func (choice interactionToolChoice) MarshalJSON() ([]byte, error) {
	if choice.AllowedTools != nil {
		return json.Marshal(struct {
			AllowedTools *interactionAllowedTools `json:"allowed_tools"`
		}{AllowedTools: choice.AllowedTools})
	}
	return json.Marshal(choice.Mode)
}

type interactionResponseFormat struct {
	Type     string          `json:"type"`
	MIMEType string          `json:"mime_type"`
	Schema   json.RawMessage `json:"schema,omitempty"`
}

type interactionTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	SearchTypes []string        `json:"search_types,omitempty"`
}

func encodeTextRequest(request provider.Request) (interactionRequest, []compat.Change, error) {
	canonicalRequest := request.Canonical
	if strings.TrimSpace(canonicalRequest.Model()) == "" { // swobu:io-string source=boundary
		return interactionRequest{}, nil, canonical.BadRequest("Gemini model is required")
	}
	previousInteractionID, inputRequest, err := geminiPreviousInteraction(request)
	if err != nil {
		return interactionRequest{}, nil, err
	}
	inputRequest, changes, err := projectSettledPortableSearchHistory(inputRequest)
	if err != nil {
		return interactionRequest{}, nil, err
	}

	encoded := interactionRequest{Model: canonicalRequest.Model(), PreviousInteractionID: previousInteractionID, Stream: true}
	if store, specified := canonicalRequest.Store(); specified {
		encoded.Store = &store
	}

	if encoded.GenerationConfig, changes, err = geminiGenerationConfig(canonicalRequest, inputRequest, request.ToolNames, changes); err != nil {
		return interactionRequest{}, changes, err
	}
	if encoded.ResponseFormat, changes, err = geminiResponseFormat(canonicalRequest.OutputFormat(), changes); err != nil {
		return interactionRequest{}, changes, err
	}
	if encoded.Tools, changes, err = geminiTools(inputRequest, request.ToolNames, changes); err != nil {
		return interactionRequest{}, changes, err
	}
	var instructions strings.Builder
	instructionCount := 0
	historyStarted := false
	for itemIndex, item := range inputRequest.Items() {
		if _, hasTools := item.ToolDeclarations(); hasTools {
			continue
		}
		if reasoning, ok := item.Reasoning(); ok {
			raw, exact := reasoning.Opaque().Interactions()
			if !exact {
				// Readable foreign reasoning has no truthful Interactions thought
				// signature. Its portable omission is already established policy.
				changes = compat.AppendUnique(changes, compat.NewOmission(canonical.RequestItemsResponsesReasoningReplay, canonical.RequestItemOccurrence(uint32(itemIndex))))
				continue
			}
			if err := validateGeminiThoughtReplay(raw); err != nil {
				return interactionRequest{}, changes, canonical.InternalError("Gemini opaque thought replay is invalid")
			}
			encoded.Input = append(encoded.Input, interactionInputStep{raw: raw})
			continue
		}
		if call, ok := item.ToolCall(); ok {
			if call.Tool().Kind() == canonical.ToolKindWebSearch {
				search, exact := call.Input().WebSearch()
				if !exact {
					return interactionRequest{}, changes, canonical.InternalError("Gemini historical search call has invalid input")
				}
				raw, exact := search.InteractionsReplay()
				if !exact {
					return interactionRequest{}, changes, provider.NewIncompatibleTarget("Gemini Google Search call lacks exact Interactions replay")
				}
				if err := validateGeminiSearchCallReplay(raw, call.CallID()); err != nil {
					return interactionRequest{}, changes, err
				}
				encoded.Input = append(encoded.Input, interactionInputStep{raw: raw})
				continue
			}
			if call.Tool().Kind() != canonical.ToolKindFunction {
				return interactionRequest{}, changes, provider.NewIncompatibleTarget("Gemini non-function tool-call history is not implemented yet")
			}
			arguments, ok := call.Input().Object()
			if !ok {
				return interactionRequest{}, changes, canonical.InternalError("Gemini historical function call has invalid input")
			}
			name, nameErr := wire.EncodeToolName(request.ToolNames, call.Tool())
			if nameErr != nil {
				return interactionRequest{}, changes, nameErr
			}
			encoded.Input = append(encoded.Input, interactionInputStep{Type: "function_call", ID: call.CallID().String(), Name: name, Arguments: json.RawMessage(arguments.Bytes())})
			continue
		}
		if result, ok := item.ToolResult(); ok {
			if search, isSearch := result.WebSearch(); isSearch {
				raw, exact := search.InteractionsReplay()
				if !exact {
					return interactionRequest{}, changes, provider.NewIncompatibleTarget("Gemini Google Search result lacks exact Interactions replay")
				}
				if err := validateGeminiSearchResultReplay(raw, result.CallID()); err != nil {
					return interactionRequest{}, changes, err
				}
				encoded.Input = append(encoded.Input, interactionInputStep{raw: raw})
				continue
			}
			step, stepChanges, stepErr := geminiFunctionResult(result, uint32(itemIndex))
			changes = append(changes, stepChanges...)
			if stepErr != nil {
				return interactionRequest{}, changes, stepErr
			}
			encoded.Input = append(encoded.Input, step)
			continue
		}
		if _, discovery := item.ToolDiscoveryResult(); discovery {
			return interactionRequest{}, changes, provider.NewIncompatibleTarget("Gemini tool-discovery history is not implemented yet")
		}
		message, ok := item.Message()
		if !ok {
			return interactionRequest{}, changes, canonical.InternalError("Gemini request contains an invalid canonical item")
		}
		switch message.Role() {
		case canonical.MessageRoleSystem, canonical.MessageRoleDeveloper:
			if instructionCount > 0 {
				instructions.WriteString("\n\n")
			}
			for _, part := range message.Content() {
				text, ok := part.Text()
				if !ok {
					return interactionRequest{}, changes, provider.IncompatibleCapability(canonical.RequestItemsMessageImage, canonical.RequestItemOccurrence(uint32(itemIndex)), "Gemini directives cannot contain image input")
				}
				instructions.WriteString(text.Text())
			}
			if message.Role() != canonical.MessageRoleSystem || instructionCount > 0 || historyStarted {
				changes = compat.AppendUnique(changes, compat.NewChange(canonical.RequestInstructions, compat.Approximation, canonical.Occurrence{}))
			}
			instructionCount++
		case canonical.MessageRoleUser, canonical.MessageRoleAssistant:
			historyStarted = true
			content, contentChanges, contentErr := geminiMessageContent(message.Content(), uint32(itemIndex))
			changes = append(changes, contentChanges...)
			if contentErr != nil {
				return interactionRequest{}, changes, contentErr
			}
			stepType := "user_input"
			if message.Role() == canonical.MessageRoleAssistant {
				stepType = "model_output"
			}
			encoded.Input = append(encoded.Input, interactionInputStep{Type: stepType, Content: content})
		default:
			return interactionRequest{}, changes, canonical.InternalError("Gemini request message role is invalid")
		}
	}
	if instructions.Len() > 0 {
		encoded.SystemInstruction = instructions.String()
	}
	if encoded.Input == nil {
		encoded.Input = []interactionInputStep{}
	}
	return encoded, changes, nil
}

// projectSettledPortableSearchHistory omits only completed foreign Search
// effects that have no exact Gemini replay on either occurrence. Active or
// partially exact effects remain for the strict item encoder to reject.
func projectSettledPortableSearchHistory(request canonical.CanonicalRequest) (canonical.CanonicalRequest, []compat.Change, error) {
	items := request.Items()
	drop := make(map[int]struct{})
	changes := make([]compat.Change, 0)
	var matcher canonical.ToolEffectMatcher
	effects := make([]canonical.ToolEffect, 0)
	for index, item := range items {
		if call, ok := item.ToolCall(); ok && call.Tool().Kind() != canonical.ToolKindWebSearch {
			continue
		}
		if _, isCall := item.ToolCall(); !isCall {
			result, ok := item.ToolResult()
			if !ok {
				continue
			}
			if _, webSearch := result.WebSearch(); !webSearch {
				continue
			}
		}
		completed, err := matcher.Accept(index, item)
		if err != nil {
			return request, nil, provider.IncompatibleCapability(
				canonical.RequestItemsKind,
				canonical.Occurrence{},
				"Gemini Interactions cannot correlate canonical web-search history",
			)
		}
		if completed != nil {
			effects = append(effects, *completed)
		}
	}
	effects = append(effects, matcher.Pending()...)
	for _, completed := range effects {
		if completed.Kind != canonical.ToolKindWebSearch {
			continue
		}
		if completed.ResultIndex < 0 {
			return request, nil, provider.IncompatibleCapability(
				canonical.RequestItemsKind,
				canonical.CallOccurrence(completed.CallID),
				"Gemini Interactions cannot represent unresolved web-search history",
			)
		}
		call, _ := items[completed.CallIndex].ToolCall()
		search, _ := call.Input().WebSearch()
		_, callExact := search.InteractionsReplay()
		result, _ := items[completed.ResultIndex].ToolResult()
		searchResult, _ := result.WebSearch()
		_, resultExact := searchResult.InteractionsReplay()
		if callExact != resultExact {
			return request, nil, provider.IncompatibleCapability(
				canonical.RequestItemsKind,
				canonical.CallOccurrence(completed.CallID),
				"Gemini Interactions requires exact replay on both web-search call and result",
			)
		}
		if callExact {
			continue
		}
		drop[completed.CallIndex] = struct{}{}
		drop[completed.ResultIndex] = struct{}{}
		changes = append(changes, compat.NewOmission(
			canonical.RequestItemsKind,
			canonical.RequestItemOccurrence(uint32(completed.CallIndex)),
		))
	}
	if len(drop) == 0 {
		return request, nil, nil
	}
	projected := make([]canonical.CanonicalItem, 0, len(items)-len(drop))
	for index, item := range items {
		if _, omitted := drop[index]; omitted {
			continue
		}
		projected = append(projected, item)
	}
	return request.WithItems(projected), changes, nil
}

func validateGeminiThoughtReplay(raw []byte) error {
	var step interactionInputStep
	if err := json.Unmarshal(raw, &step); err != nil || strings.TrimSpace(step.Type) != "thought" || strings.TrimSpace(step.Signature) == "" {
		return provider.NewIncompatibleTarget("Gemini thought replay is malformed")
	}
	return nil
}

func validateGeminiSearchCallReplay(raw []byte, callID canonical.ToolCallID) error {
	var step struct {
		Type       string          `json:"type"`
		ID         string          `json:"id"`
		Arguments  json.RawMessage `json:"arguments"`
		SearchType string          `json:"search_type"`
		Signature  string          `json:"signature"`
	}
	if err := json.Unmarshal(raw, &step); err != nil ||
		strings.TrimSpace(step.Type) != "google_search_call" ||
		strings.TrimSpace(step.ID) != callID.String() ||
		len(step.Arguments) == 0 ||
		strings.TrimSpace(step.SearchType) != "web_search" ||
		strings.TrimSpace(step.Signature) == "" {
		return provider.NewIncompatibleTarget("Gemini Google Search call replay is malformed or mismatched")
	}
	return nil
}

func validateGeminiSearchResultReplay(raw []byte, callID canonical.ToolCallID) error {
	var step struct {
		Type      string          `json:"type"`
		CallID    string          `json:"call_id"`
		Result    json.RawMessage `json:"result"`
		Signature string          `json:"signature"`
	}
	if err := json.Unmarshal(raw, &step); err != nil ||
		strings.TrimSpace(step.Type) != "google_search_result" ||
		strings.TrimSpace(step.CallID) != callID.String() ||
		len(step.Result) == 0 ||
		strings.TrimSpace(step.Signature) == "" {
		return provider.NewIncompatibleTarget("Gemini Google Search result replay is malformed or mismatched")
	}
	return nil
}

func geminiGenerationConfig(request canonical.CanonicalRequest, input canonical.CanonicalRequest, names wire.ToolNames, changes []compat.Change) (*interactionGenerationConfig, []compat.Change, error) {
	controls := request.Controls()
	if compute, specified := request.Reasoning().ComputeField().Get(); specified && compute.Kind() == canonical.ReasoningDisabled {
		return nil, changes, provider.IncompatibleCapability(
			canonical.RequestReasoning,
			canonical.Occurrence{},
			"Gemini Interactions cannot represent hard-off reasoning",
		)
	}
	config := &interactionGenerationConfig{}
	if value, ok := controls.Limits.MaxOutputTokens.Value(); ok {
		config.MaxOutputTokens = &value
	}
	if len(controls.Limits.StopSequences) > 0 {
		config.StopSequences = append([]string(nil), controls.Limits.StopSequences...)
	}
	if value, ok := controls.Sampling.Temperature.Value(); ok {
		config.Temperature = &value
	}
	if value, ok := controls.Sampling.TopP.Value(); ok {
		config.TopP = &value
	}

	thinkingLevel, thinkingChanges := geminiThinkingLevel(request)
	changes = append(changes, thinkingChanges...)
	config.ThinkingLevel = thinkingLevel
	if disclosure, ok := request.Reasoning().DisclosureField().Get(); ok {
		switch disclosure {
		case canonical.ReasoningDisclosureSummary:
			config.ThinkingSummaries = "auto"
		case canonical.ReasoningDisclosureNone:
			config.ThinkingSummaries = "none"
		}
	}
	if request.Reasoning().ResponsesContextField().IsSpecified() {
		changes = compat.AppendUnique(changes, compat.NewOmission(canonical.RequestReasoningContextResponses, canonical.Occurrence{}))
	}

	policy, err := request.EffectiveToolPolicy()
	if err != nil {
		return nil, changes, err
	}
	environment, environmentErr := canonical.EffectiveTools(input)
	if environmentErr != nil {
		return nil, changes, canonical.InternalError("Gemini function environment is ambiguous")
	}
	var flat wire.FlatToolSet
	if !environment.IsEmpty() || request.ToolPolicySpecified() {
		var flattenErr error
		flat, flattenErr = wire.PrepareFlatToolSet(environment.Declarations(), func(tool canonical.ToolDeclaration) (string, error) {
			if tool.Kind() != canonical.ToolKindFunction && tool.Kind() != canonical.ToolKindCustom {
				return string(tool.Kind()), nil
			}
			if tool.Kind() == canonical.ToolKindCustom {
				return string(tool.Kind()) + "\x00" + tool.Key().String(), nil
			}
			name, nameErr := wire.EncodeToolName(names, tool.Key())
			return string(tool.Kind()) + "\x00" + strings.TrimSpace(name), nameErr
		})
		if flattenErr != nil {
			return nil, changes, flattenErr
		}
		choice, choiceErr := geminiToolChoice(policy, flat.Declarations, names)
		if choiceErr != nil {
			return nil, changes, choiceErr
		}
		config.ToolChoice = &choice
	}
	if request.ToolCallBatch().Mode == canonical.ToolCallBatchAtMostOne && policy.Mode != canonical.ToolPolicyNone && len(flat.Declarations) > 0 {
		return nil, changes, provider.IncompatibleCapability(
			canonical.RequestToolCallBatch,
			canonical.Occurrence{},
			"Gemini Interactions cannot enforce at-most-one tool call with active callable tools",
		)
	}
	if config.MaxOutputTokens == nil && len(config.StopSequences) == 0 && config.Temperature == nil && config.TopP == nil && config.ThinkingLevel == "" && config.ThinkingSummaries == "" && config.ToolChoice == nil {
		return nil, changes, nil
	}
	return config, changes, nil
}

func geminiThinkingLevel(request canonical.CanonicalRequest) (string, []compat.Change) {
	projection := reasoningprojection.ProjectOrdinalReasoning(request.Reasoning(), request.Controls().Effort)
	if projection.Kind != reasoningprojection.OrdinalEffort {
		return "", projection.Changes
	}
	value := string(projection.Effort)
	changes := projection.Changes
	switch value {
	case string(canonical.InferenceEffortLow), string(canonical.InferenceEffortMedium), string(canonical.InferenceEffortHigh):
		return value, changes
	case string(canonical.InferenceEffortMinimal):
		changes = compat.AppendUnique(changes, geminiEffortApproximation())
		return string(canonical.InferenceEffortLow), changes
	case string(canonical.InferenceEffortXHigh), string(canonical.InferenceEffortMax):
		changes = compat.AppendUnique(changes, geminiEffortApproximation())
		return string(canonical.InferenceEffortHigh), changes
	default:
		panic("shared ordinal reasoning projection returned an unknown value")
	}
}

func geminiEffortApproximation() compat.Change {
	return compat.NewApproximation(canonical.RequestControlsEffort, canonical.RequestControlsEffort, canonical.Occurrence{})
}

func geminiToolChoice(policy canonical.ToolPolicy, tools []canonical.ToolDeclaration, names wire.ToolNames) (interactionToolChoice, error) {
	switch policy.Mode {
	case canonical.ToolPolicyNone:
		return interactionToolChoice{Mode: "none"}, nil
	case canonical.ToolPolicyAuto:
		return interactionToolChoice{Mode: "auto"}, nil
	case canonical.ToolPolicyRequired:
		return interactionToolChoice{Mode: "any"}, nil
	case canonical.ToolPolicySpecific:
		specific, ok := policy.SpecificID()
		if !ok {
			return interactionToolChoice{}, canonical.InternalError("Gemini specific tool policy is missing its key")
		}
		declaration, _, err := canonical.ResolveToolDeclarationByKey(tools, specific, string(specific.Kind()))
		if err != nil {
			return interactionToolChoice{}, provider.IncompatibleCapability(canonical.RequestToolPolicy, canonical.Occurrence{}, "Gemini specific tool policy does not survive the native function surface")
		}
		if declaration.Kind() != canonical.ToolKindFunction {
			return interactionToolChoice{}, provider.IncompatibleCapability(canonical.RequestToolPolicy, canonical.Occurrence{}, "Gemini specific tool policy requires an ordinary function")
		}
		name, err := wire.EncodeToolName(names, specific)
		if err != nil {
			return interactionToolChoice{}, err
		}
		return interactionToolChoice{AllowedTools: &interactionAllowedTools{Mode: "any", Tools: []string{name}}}, nil
	default:
		return interactionToolChoice{}, canonical.InternalError("Gemini tool policy is invalid")
	}
}

func geminiResponseFormat(format canonical.OutputFormat, changes []compat.Change) (*interactionResponseFormat, []compat.Change, error) {
	if format.IsZero() || format.Kind == canonical.OutputFormatText {
		return nil, changes, nil
	}
	if err := format.Validate(); err != nil {
		return nil, changes, err
	}
	responseFormat := &interactionResponseFormat{Type: "text", MIMEType: "application/json"}
	switch format.Kind {
	case canonical.OutputFormatJSONObject:
		return responseFormat, changes, nil
	case canonical.OutputFormatJSONSchema:
		responseFormat.Schema = json.RawMessage(format.Schema.RawObject())
		if !format.Strict {
			changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestOutputFormatSchema, canonical.RequestOutputFormat, canonical.Occurrence{}))
		}
		return responseFormat, changes, nil
	default:
		return nil, changes, provider.IncompatibleCapability(canonical.RequestOutputFormat, canonical.Occurrence{}, "Gemini cannot represent the canonical output format")
	}
}

func geminiTools(request canonical.CanonicalRequest, names wire.ToolNames, changes []compat.Change) ([]interactionTool, []compat.Change, error) {
	environment, err := canonical.EffectiveTools(request)
	if err != nil {
		return nil, changes, canonical.InternalError("Gemini function environment is ambiguous")
	}
	flat, err := wire.PrepareFlatToolSet(environment.Declarations(), func(tool canonical.ToolDeclaration) (string, error) {
		if tool.Kind() != canonical.ToolKindFunction && tool.Kind() != canonical.ToolKindCustom {
			return string(tool.Kind()), nil
		}
		if tool.Kind() == canonical.ToolKindCustom {
			return string(tool.Kind()) + "\x00" + tool.Key().String(), nil
		}
		name, nameErr := wire.EncodeToolName(names, tool.Key())
		return string(tool.Kind()) + "\x00" + strings.TrimSpace(name), nameErr
	})
	if err != nil {
		return nil, changes, err
	}
	if flat.RemovedNamespaces > 0 {
		changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestTools, canonical.RequestTools, canonical.Occurrence{}))
	}
	if len(flat.Declarations) == 0 {
		return nil, changes, nil
	}
	tools := make([]interactionTool, 0, len(flat.Declarations))
	for _, declaration := range flat.Declarations {
		if declaration.Kind() == canonical.ToolKindWebSearch {
			// Canonical owns web search only. Explicitly selecting the provider's
			// web mode avoids silently enabling image or enterprise search.
			tools = append(tools, interactionTool{Type: "google_search", SearchTypes: []string{"web_search"}})
			continue
		}
		function, ok := declaration.Function()
		if !ok {
			return nil, changes, provider.IncompatibleCapability(canonical.RequestToolsKind, canonical.ToolOccurrence(declaration.Key()), "Gemini supports only ordinary functions and web search")
		}
		if _, specified := function.Strict().Get(); specified {
			return nil, changes, provider.IncompatibleCapability(canonical.RequestToolsSchemaStrict, canonical.ToolOccurrence(function.Key()), "Gemini function declarations have no exact strictness carrier")
		}
		name, err := wire.EncodeToolName(names, function.Key())
		if err != nil {
			return nil, changes, err
		}
		parameters := strings.TrimSpace(function.InputSchema().RawObject())
		if parameters == "" {
			return nil, changes, canonical.InternalError("Gemini function declaration has no schema")
		}
		tools = append(tools, interactionTool{Type: "function", Name: name, Description: function.Description(), Parameters: json.RawMessage(parameters)})
	}
	return tools, changes, nil
}

func geminiMessageContent(parts []canonical.MessagePart, item uint32) ([]interactionContent, []compat.Change, error) {
	content := make([]interactionContent, 0, len(parts))
	var changes []compat.Change
	for partIndex, part := range parts {
		if text, ok := part.Text(); ok {
			content = append(content, interactionContent{Type: "text", Text: text.Text()})
			continue
		}
		image, ok := part.Image()
		if !ok {
			return nil, changes, provider.IncompatibleCapability(canonical.RequestItemsKind, canonical.RequestItemOccurrence(item), "Gemini cannot represent this message content")
		}
		encoded, imageChanges, err := geminiImageContent(image, canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: item, Part: uint32(partIndex)}), canonical.RequestItemsMessageImageDetail)
		changes = append(changes, imageChanges...)
		if err != nil {
			return nil, changes, err
		}
		content = append(content, encoded)
	}
	return content, changes, nil
}

func geminiFunctionResult(result canonical.ToolResultItem, item uint32) (interactionInputStep, []compat.Change, error) {
	if _, webSearch := result.WebSearch(); webSearch {
		return interactionInputStep{}, nil, provider.IncompatibleCapability(canonical.RequestItemsToolResultContent, canonical.RequestItemOccurrence(item), "Gemini native Google Search results are not implemented yet")
	}
	contents := make([]interactionContent, 0, len(result.Content()))
	for partIndex, part := range result.Content() {
		text, ok := part.Text()
		if !ok {
			return interactionInputStep{}, nil, provider.IncompatibleCapability(canonical.RequestItemsToolResultContent, canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: item, Part: uint32(partIndex)}), "Gemini function-result image replay is not implemented yet")
		}
		contents = append(contents, interactionContent{Type: "text", Text: text.Text()})
	}
	isError := result.IsError()
	return interactionInputStep{Type: "function_result", CallID: result.CallID().String(), Result: contents, IsError: &isError}, nil, nil
}

func geminiImageContent(image canonical.ImagePart, occurrence canonical.Occurrence, detailCapability canonical.CapabilityPath) (interactionContent, []compat.Change, error) {
	var changes []compat.Change
	if image.Detail().IsSpecified() {
		changes = append(changes, compat.NewApproximation(detailCapability, canonical.RequestItemsMessageImage, occurrence))
	}
	if url, ok := image.Source().URL(); ok {
		return interactionContent{Type: "image", URI: url.String()}, changes, nil
	}
	if inline, ok := image.Source().Inline(); ok {
		return interactionContent{Type: "image", Data: base64.StdEncoding.EncodeToString(inline.Data()), MIMEType: string(inline.MediaType())}, changes, nil
	}
	return interactionContent{}, changes, canonical.InternalError("Gemini image source is invalid")
}

// geminiPreviousInteraction selects only Gemini's closed continuation child.
// Session owns exact-target and persistence eligibility; this adapter validates
// its granted projection before it removes canonical history from the wire.
func geminiPreviousInteraction(request provider.Request) (string, canonical.CanonicalRequest, error) {
	canonicalRequest := request.Canonical
	previous := request.PreviousHistory
	if previous == nil || previous.Response.Interactions == nil {
		return "", canonicalRequest, nil
	}
	if previous.Response.Responses != nil {
		return "", canonical.CanonicalRequest{}, canonical.InternalError("Gemini previous history contains multiple native continuations")
	}
	if !canonicalRequest.PersistenceEligible() {
		return "", canonical.CanonicalRequest{}, canonical.InternalError("Gemini previous history contradicts store:false")
	}
	continuation := *previous.Response.Interactions
	if err := continuation.ValidateBound(); err != nil {
		return "", canonical.CanonicalRequest{}, canonical.InternalError("Gemini previous history contains an invalid interaction continuation")
	}

	items := canonicalRequest.Items()
	if previous.OmitStart > previous.OmitEnd || uint64(previous.OmitEnd) > uint64(len(items)) {
		return "", canonical.CanonicalRequest{}, canonical.InternalError("Gemini previous history contains an invalid item range")
	}
	prelude, _, err := canonical.SplitRequestPrelude(items)
	if err != nil {
		return "", canonical.CanonicalRequest{}, canonical.InternalError("Gemini request has an invalid request-scoped prelude")
	}
	if uint64(previous.OmitStart) != uint64(len(prelude.Items())) {
		return "", canonical.CanonicalRequest{}, canonical.InternalError("Gemini previous history range does not follow the request prelude")
	}

	projected := make([]canonical.CanonicalItem, 0, len(items)-int(previous.OmitEnd-previous.OmitStart))
	projected = append(projected, items[:previous.OmitStart]...)
	projected = append(projected, items[previous.OmitEnd:]...)
	return continuation.ProviderInteractionID.String(), canonicalRequest.WithItems(projected), nil
}

var _ provider.Codec = codec{}
