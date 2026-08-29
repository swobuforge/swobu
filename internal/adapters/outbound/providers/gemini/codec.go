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
		return carrier.Document{}, nil, canonical.BadEndpoint("Gemini target requires SSE streaming delivery")
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
	inputRequest := canonicalRequest
	var err error
	inputRequest, changes, err := projectSettledPortableSearchHistory(inputRequest)
	if err != nil {
		return interactionRequest{}, nil, err
	}
	inputRequest, discoveryChanges, err := projectGeminiProviderDiscoveryHistory(inputRequest)
	if err != nil {
		return interactionRequest{}, nil, err
	}
	changes = append(changes, discoveryChanges...)

	encoded := interactionRequest{Model: canonicalRequest.Model(), Stream: true}
	if store, specified := canonicalRequest.Store(); specified {
		encoded.Store = &store
	}

	var loweredTools wire.LoweredToolSet
	if encoded.Tools, loweredTools, changes, err = geminiTools(inputRequest, request.ToolNames, changes); err != nil {
		return interactionRequest{}, changes, err
	}
	inputRequest, historyChanges, err := projectGeminiUnloweredToolHistory(inputRequest, loweredTools)
	if err != nil {
		return interactionRequest{}, changes, err
	}
	changes = append(changes, historyChanges...)
	// Correlation is established against complete canonical history before a
	// native continuation handle compresses its represented prefix.
	functionNames := make(map[canonical.ToolCallID]string)
	for _, item := range inputRequest.Items() {
		call, ok := item.ToolCall()
		if !ok || call.Tool().Kind() == canonical.ToolKindWebSearch {
			continue
		}
		name, nameErr := request.ToolNames.WireName(call.Tool())
		if nameErr != nil {
			return interactionRequest{}, changes, nameErr
		}
		functionNames[call.CallID()] = name
	}
	projectedProviderRequest := request
	projectedProviderRequest.Canonical = inputRequest
	encoded.PreviousInteractionID, inputRequest, err = geminiPreviousInteraction(projectedProviderRequest)
	if err != nil {
		return interactionRequest{}, changes, err
	}
	if encoded.GenerationConfig, changes, err = geminiGenerationConfig(canonicalRequest, loweredTools, request.ToolNames, changes); err != nil {
		return interactionRequest{}, changes, err
	}
	if encoded.ResponseFormat, changes, err = geminiResponseFormat(canonicalRequest.OutputFormat(), changes); err != nil {
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
					return interactionRequest{}, changes, canonical.InternalError("Gemini received an invalid portable Google Search call")
				}
				if err := validateGeminiSearchCallReplay(raw, call.CallID()); err != nil {
					return interactionRequest{}, changes, err
				}
				encoded.Input = append(encoded.Input, interactionInputStep{raw: raw})
				continue
			}
			record, found := loweredTools.FindSource(call.Tool())
			if !found || record.FragmentCount != 1 {
				return interactionRequest{}, changes, canonical.InternalError("Gemini retained callable history has no unique lowered identity")
			}
			arguments, ok := call.Input().Object()
			var wireArguments json.RawMessage
			if ok {
				wireArguments = json.RawMessage(arguments.Bytes())
			} else if text, textInput := call.Input().Text(); textInput {
				wireArguments, err = json.Marshal(map[string]string{"input": text})
				if err != nil {
					return interactionRequest{}, changes, canonical.InternalError("Gemini historical callable input encoding failed")
				}
				changes = compat.AppendUnique(changes, compat.NewApproximation(
					canonical.RequestItemsToolCallInput,
					canonical.RequestItemOccurrence(uint32(itemIndex)),
				))
			} else {
				return interactionRequest{}, changes, canonical.InternalError("Gemini historical callable has invalid input")
			}
			name, nameErr := wire.EncodeToolName(request.ToolNames, call.Tool())
			if nameErr != nil {
				return interactionRequest{}, changes, nameErr
			}
			functionNames[call.CallID()] = name
			encoded.Input = append(encoded.Input, interactionInputStep{Type: "function_call", ID: call.CallID().String(), Name: name, Arguments: wireArguments})
			continue
		}
		if result, ok := item.ToolResult(); ok {
			if search, isSearch := result.WebSearch(); isSearch {
				raw, exact := search.InteractionsReplay()
				if !exact {
					return interactionRequest{}, changes, canonical.InternalError("Gemini received an invalid portable Google Search result")
				}
				if err := validateGeminiSearchResultReplay(raw, result.CallID()); err != nil {
					return interactionRequest{}, changes, err
				}
				encoded.Input = append(encoded.Input, interactionInputStep{raw: raw})
				continue
			}
			name, found := functionNames[result.CallID()]
			if !found {
				return interactionRequest{}, changes, canonical.InternalError("Gemini function result has no retained lowered call")
			}
			step, stepChanges, stepErr := geminiFunctionResult(result, name, uint32(itemIndex))
			changes = append(changes, stepChanges...)
			if stepErr != nil {
				return interactionRequest{}, changes, stepErr
			}
			encoded.Input = append(encoded.Input, step)
			continue
		}
		if result, discovery := item.ToolDiscoveryResult(); discovery {
			name, found := functionNames[result.CallID()]
			if !found {
				return interactionRequest{}, changes, canonical.InternalError("Gemini discovery result has no retained lowered call")
			}
			step, stepErr := geminiDiscoveryResult(result, name)
			if stepErr != nil {
				return interactionRequest{}, changes, stepErr
			}
			encoded.Input = append(encoded.Input, step)
			continue
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
					return interactionRequest{}, changes, canonical.InternalError("Gemini received an invalid canonical image directive")
				}
				instructions.WriteString(text.Text())
			}
			if message.Role() != canonical.MessageRoleSystem || instructionCount > 0 || historyStarted {
				changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestInstructions, canonical.Occurrence{}))
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

// projectGeminiUnloweredToolHistory makes historical callable effects obey the
// exact declaration lowering used for this request. A call and its result are
// one correlation unit: zero-fragment sources are omitted atomically, while a
// multi-fragment source cannot provide the single identity history requires.
func projectGeminiUnloweredToolHistory(request canonical.CanonicalRequest, lowered wire.LoweredToolSet) (canonical.CanonicalRequest, []compat.Change, error) {
	items := request.Items()
	effects, err := canonical.MatchToolEffects(items)
	if err != nil {
		return canonical.CanonicalRequest{}, nil, canonical.InternalError("canonical tool history contains an orphan result")
	}
	drop := make(map[int]struct{})
	changes := make([]compat.Change, 0)
	for _, effect := range effects {
		call, ok := items[effect.CallIndex].ToolCall()
		if !ok {
			return canonical.CanonicalRequest{}, nil, canonical.InternalError("canonical tool history contains an invalid call effect")
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
			return canonical.CanonicalRequest{}, nil, canonical.InternalError("Gemini callable history source lowered to multiple wire call identities")
		default:
			drop[effect.CallIndex] = struct{}{}
			if effect.ResultIndex >= 0 {
				drop[effect.ResultIndex] = struct{}{}
			}
			changes = append(changes, compat.NewOmission(
				canonical.RequestItemsKind,
				canonical.RequestItemOccurrence(uint32(effect.CallIndex)),
			))
		}
	}
	if len(drop) == 0 {
		return request, nil, nil
	}
	projected := make([]canonical.CanonicalItem, 0, len(items)-len(drop))
	for index, item := range items {
		if _, omitted := drop[index]; !omitted {
			projected = append(projected, item)
		}
	}
	return request.WithItems(projected), changes, nil
}

// projectSettledPortableSearchHistory keeps only complete effects whose call
// and result both carry exact Gemini replay. Every other valid Search effect is
// omitted atomically at its call occurrence.
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
			return request, nil, canonical.InternalError("canonical web-search history cannot be correlated for Gemini")
		}
		if completed != nil {
			effects = append(effects, *completed)
		}
	}
	effects = append(effects, matcher.Pending()...)
	for _, effect := range effects {
		if effect.Kind != canonical.ToolKindWebSearch {
			continue
		}
		call, _ := items[effect.CallIndex].ToolCall()
		search, _ := call.Input().WebSearch()
		callReplay, callExact := search.InteractionsReplay()
		if callExact {
			if err := validateGeminiSearchCallReplay(callReplay, call.CallID()); err != nil {
				return request, nil, err
			}
		}
		resultExact := false
		if effect.ResultIndex >= 0 {
			result, _ := items[effect.ResultIndex].ToolResult()
			searchResult, _ := result.WebSearch()
			resultReplay, exact := searchResult.InteractionsReplay()
			resultExact = exact
			if resultExact {
				if err := validateGeminiSearchResultReplay(resultReplay, result.CallID()); err != nil {
					return request, nil, err
				}
			}
		}
		if callExact && (effect.ResultIndex < 0 || resultExact) {
			continue
		}
		drop[effect.CallIndex] = struct{}{}
		if effect.ResultIndex >= 0 {
			drop[effect.ResultIndex] = struct{}{}
		}
		changes = append(changes, compat.NewOmission(
			canonical.RequestItemsKind,
			canonical.RequestItemOccurrence(uint32(effect.CallIndex)),
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

// projectGeminiProviderDiscoveryHistory replaces a settled provider-owned
// search with eager tool visibility. Client discovery remains a generic
// function call/result pair and is not changed here.
func projectGeminiProviderDiscoveryHistory(request canonical.CanonicalRequest) (canonical.CanonicalRequest, []compat.Change, error) {
	drop := map[int]struct{}{}
	var matcher canonical.ToolEffectMatcher
	for index, item := range request.Items() {
		if call, ok := item.ToolCall(); ok && call.Tool().Kind() != canonical.ToolKindDiscovery {
			continue
		}
		if _, call := item.ToolCall(); !call {
			result, ok := item.ToolDiscoveryResult()
			if !ok || result.Executor() != canonical.DiscoveryExecutorProvider {
				continue
			}
		}
		completed, err := matcher.Accept(index, item)
		if err != nil {
			return request, nil, canonical.InternalError("canonical tool history cannot be correlated for Gemini")
		}
		if completed == nil || completed.Kind != canonical.ToolKindDiscovery {
			continue
		}
		executor, _ := completed.Executor.Get()
		if executor == canonical.DiscoveryExecutorProvider {
			drop[completed.CallIndex] = struct{}{}
			drop[completed.ResultIndex] = struct{}{}
		}
	}
	if len(drop) == 0 {
		return request, nil, nil
	}
	items := request.Items()
	projected := make([]canonical.CanonicalItem, 0, len(items)-len(drop))
	for index, item := range items {
		if _, remove := drop[index]; !remove {
			projected = append(projected, item)
		}
	}
	return request.WithItems(projected), []compat.Change{compat.NewApproximation(canonical.RequestItemsKind, canonical.Occurrence{})}, nil
}

func validateGeminiThoughtReplay(raw []byte) error {
	var step interactionInputStep
	if err := json.Unmarshal(raw, &step); err != nil || strings.TrimSpace(step.Type) != "thought" || strings.TrimSpace(step.Signature) == "" {
		return canonical.InternalError("Gemini thought replay is malformed")
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
		return canonical.InternalError("Gemini Google Search call replay is malformed or mismatched")
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
		return canonical.InternalError("Gemini Google Search result replay is malformed or mismatched")
	}
	return nil
}

func geminiGenerationConfig(request canonical.CanonicalRequest, lowered wire.LoweredToolSet, names wire.ToolNames, changes []compat.Change) (*interactionGenerationConfig, []compat.Change, error) {
	controls := request.Controls()
	if compute, specified := request.Reasoning().ComputeField().Get(); specified && compute.Kind() == canonical.ReasoningDisabled {
		changes = compat.AppendUnique(changes, compat.NewOmission(canonical.RequestReasoning, canonical.Occurrence{}))
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
	if lowered.Len() > 0 || request.ToolPolicySpecified() {
		choice, represented, choiceErr := geminiToolChoice(policy, lowered, names)
		if choiceErr != nil {
			return nil, changes, choiceErr
		}
		if !represented {
			changes = compat.AppendUnique(changes, compat.NewOmission(canonical.RequestToolPolicy, canonical.Occurrence{}))
		} else if lowered.TotalFragments() > 0 {
			config.ToolChoice = &choice
		}
	}
	if request.ToolCallBatch().Mode == canonical.ToolCallBatchAtMostOne && policy.Mode != canonical.ToolPolicyNone && lowered.TotalFragments() > 0 {
		changes = compat.AppendUnique(
			changes,
			compat.NewOmission(
				canonical.RequestToolCallBatch,
				canonical.Occurrence{},
			),
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
	return compat.NewApproximation(canonical.RequestControlsEffort, canonical.Occurrence{})
}

func geminiToolChoice(policy canonical.ToolPolicy, lowered wire.LoweredToolSet, names wire.ToolNames) (interactionToolChoice, bool, error) {
	switch policy.Mode {
	case canonical.ToolPolicyNone:
		return interactionToolChoice{Mode: "none"}, true, nil
	case canonical.ToolPolicyAuto:
		return interactionToolChoice{Mode: "auto"}, true, nil
	case canonical.ToolPolicyRequired:
		if lowered.TotalFragments() == 0 {
			return interactionToolChoice{}, false, nil
		}
		return interactionToolChoice{Mode: "any"}, true, nil
	case canonical.ToolPolicySpecific:
		key, ok := policy.SpecificID()
		if !ok {
			return interactionToolChoice{}, false, canonical.BadRequest("specific tool policy requires a tool id")
		}
		record, ok := lowered.FindSource(key)
		if !ok || record.FragmentCount != 1 {
			return interactionToolChoice{}, false, nil
		}
		name, err := wire.EncodeToolName(names, record.Key)
		if err != nil {
			return interactionToolChoice{}, false, err
		}
		return interactionToolChoice{AllowedTools: &interactionAllowedTools{Mode: "any", Tools: []string{name}}}, true, nil
	default:
		return interactionToolChoice{}, false, canonical.InternalError("Gemini tool policy is invalid")
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
			changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestOutputFormatSchema, canonical.Occurrence{}))
		}
		return responseFormat, changes, nil
	default:
		return nil, changes, canonical.InternalError("canonical output format kind is invalid")
	}
}

func geminiTools(request canonical.CanonicalRequest, names wire.ToolNames, changes []compat.Change) ([]interactionTool, wire.LoweredToolSet, []compat.Change, error) {
	environment, err := canonical.EffectiveTools(request)
	if err != nil {
		return nil, wire.LoweredToolSet{}, changes, canonical.InternalError("Gemini function environment is ambiguous")
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
		return nil, wire.LoweredToolSet{}, changes, err
	}
	if flat.RemovedNamespaces > 0 {
		changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestTools, canonical.Occurrence{}))
	}
	if len(flat.Declarations) == 0 {
		return nil, wire.LoweredToolSet{}, changes, nil
	}
	tools := make([]interactionTool, 0, len(flat.Declarations))
	lowered := wire.LoweredToolSet{Records: make([]wire.LoweredToolRecord, 0, len(flat.Declarations))}
	for _, declaration := range flat.Declarations {
		if declaration.Kind() == canonical.ToolKindWebSearch {
			// Canonical owns web search only. Explicitly selecting the provider's
			// web mode avoids silently enabling image or enterprise search.
			tools = append(tools, interactionTool{Type: "google_search", SearchTypes: []string{"web_search"}})
			lowered.Records = append(lowered.Records, wire.LoweredToolRecord{Key: declaration.Key(), Kind: declaration.Kind(), FragmentCount: 1})
			continue
		}
		function, ok := declaration.Function()
		if !ok {
			if discovery, client := declaration.Discovery(); client && discovery.Executor() == canonical.DiscoveryExecutorClient {
				name, err := wire.EncodeToolName(names, declaration.Key())
				if err != nil {
					return nil, wire.LoweredToolSet{}, changes, err
				}
				parameters := strings.TrimSpace(discovery.InputSchema().RawObject())
				if parameters == "" {
					return nil, wire.LoweredToolSet{}, changes, canonical.InternalError("Gemini discovery declaration has no schema")
				}
				tools = append(tools, interactionTool{Type: "function", Name: name, Description: discovery.Description(), Parameters: json.RawMessage(parameters)})
				lowered.Records = append(lowered.Records, wire.LoweredToolRecord{Key: declaration.Key(), Kind: declaration.Kind(), FragmentCount: 1})
				continue
			}
			if _, providerDiscovery := declaration.Discovery(); providerDiscovery {
				changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestToolsVisibility, canonical.Occurrence{}))
				continue
			}
			changes = compat.AppendUnique(changes, compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(declaration.Key())))
			lowered.Records = append(lowered.Records, wire.LoweredToolRecord{Key: declaration.Key(), Kind: declaration.Kind()})
			continue
		}
		if strict, specified := function.Strict().Get(); specified && strict {
			changes = compat.AppendUnique(changes, compat.NewOmission(canonical.RequestToolsSchemaStrict, canonical.ToolOccurrence(declaration.Key())))
		}
		name, err := wire.EncodeToolName(names, function.Key())
		if err != nil {
			return nil, wire.LoweredToolSet{}, changes, err
		}
		parameters := strings.TrimSpace(function.InputSchema().RawObject())
		if parameters == "" {
			return nil, wire.LoweredToolSet{}, changes, canonical.InternalError("Gemini function declaration has no schema")
		}
		tools = append(tools, interactionTool{Type: "function", Name: name, Description: function.Description(), Parameters: json.RawMessage(parameters)})
		lowered.Records = append(lowered.Records, wire.LoweredToolRecord{Key: declaration.Key(), Kind: declaration.Kind(), FragmentCount: 1})
	}
	return tools, lowered, changes, nil
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
			return nil, changes, canonical.InternalError("Gemini received an invalid canonical message-content variant")
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

func geminiFunctionResult(result canonical.ToolResultItem, functionName string, item uint32) (interactionInputStep, []compat.Change, error) {
	if _, webSearch := result.WebSearch(); webSearch {
		return interactionInputStep{}, nil, canonical.InternalError("Gemini received an invalid native Google Search result")
	}
	contents := make([]interactionContent, 0, len(result.Content()))
	changes := make([]compat.Change, 0)
	for _, part := range result.Content() {
		if text, ok := part.Text(); ok {
			contents = append(contents, interactionContent{Type: "text", Text: text.Text()})
			continue
		}
		image, ok := part.Image()
		if !ok {
			return interactionInputStep{}, changes, canonical.InternalError("Gemini function result contains an invalid canonical part")
		}
		occurrence := canonical.RequestItemOccurrence(item)
		encoded, imageChanges, err := geminiImageContent(image, occurrence, canonical.RequestItemsToolResultImageDetail)
		changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestItemsToolResultImage, occurrence))
		changes = append(changes, imageChanges...)
		if err != nil {
			return interactionInputStep{}, changes, err
		}
		contents = append(contents, encoded)
	}
	isError := result.IsError()
	return interactionInputStep{Type: "function_result", Name: functionName, CallID: result.CallID().String(), Result: contents, IsError: &isError}, changes, nil
}

func geminiDiscoveryResult(result canonical.ToolDiscoveryResultItem, functionName string) (interactionInputStep, error) {
	if failure, failed := result.Failure(); failed {
		isError := true
		return interactionInputStep{Type: "function_result", Name: functionName, CallID: result.CallID().String(), Result: []interactionContent{{Type: "text", Text: failure.Message()}}, IsError: &isError}, nil
	}
	loaded := make([]string, 0, len(result.Tools().Declarations()))
	for _, declaration := range result.Tools().Declarations() {
		loaded = append(loaded, declaration.Key().String())
	}
	raw, err := json.Marshal(map[string]any{"loaded_tools": loaded})
	if err != nil {
		return interactionInputStep{}, canonical.InternalError("Gemini discovery result could not be encoded")
	}
	isError := false
	return interactionInputStep{Type: "function_result", Name: functionName, CallID: result.CallID().String(), Result: []interactionContent{{Type: "text", Text: string(raw)}}, IsError: &isError}, nil
}

func geminiImageContent(image canonical.ImagePart, occurrence canonical.Occurrence, detailCapability canonical.CapabilityPath) (interactionContent, []compat.Change, error) {
	var changes []compat.Change
	if image.Detail().IsSpecified() {
		changes = append(changes, compat.NewApproximation(detailCapability, occurrence))
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
