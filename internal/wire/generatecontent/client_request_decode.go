package generatecontent

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

type ClientRequestDecoder struct{ ImageLimits shared.ImageDecodeLimitPolicy }

func (decoder ClientRequestDecoder) DecodeClientRequest(doc carrier.Document, operation canonical.ClientOperation) (wire.ClientDecodeResult, error) {
	if operation.Family != canonical.ClientFamilyGenerateContent {
		return wire.ClientDecodeResult{}, canonical.BadRequest("GenerateContent request requires its parsed client operation")
	}
	var request requestDTO
	if err := decodeClosed(doc.RawBytes(), &request); err != nil {
		return wire.ClientDecodeResult{}, err
	}
	if request.CachedContent.Present || request.ServiceTier.Present || request.Store.Present {
		return wire.ClientDecodeResult{}, canonical.BadRequest("GenerateContent request contains unsupported provider-scoped semantics")
	}
	if request.SafetySettings.Present {
		var settings []json.RawMessage
		if !bytes.Equal(bytes.TrimSpace(request.SafetySettings.Raw), []byte("null")) {
			if err := json.Unmarshal(request.SafetySettings.Raw, &settings); err != nil {
				return wire.ClientDecodeResult{}, canonical.BadRequest("GenerateContent safetySettings must be an array")
			}
		}
		if len(settings) != 0 {
			return wire.ClientDecodeResult{}, canonical.BadRequest("GenerateContent safetySettings are unsupported")
		}
	}
	var changes []compat.Change
	items, err := decodeItems(request, decoder.ImageLimits, &changes)
	if err != nil {
		return wire.ClientDecodeResult{}, err
	}
	controls, err := decodeControls(request.GenerationConfig, &changes)
	if err != nil {
		return wire.ClientDecodeResult{}, err
	}
	toolPolicy, err := decodeToolPolicy(request.ToolConfig)
	if err != nil {
		return wire.ClientDecodeResult{}, err
	}
	outputFormat, err := decodeOutputFormat(request.GenerationConfig)
	if err != nil {
		return wire.ClientDecodeResult{}, err
	}
	reasoning, err := decodeReasoning(request.GenerationConfig)
	if err != nil {
		return wire.ClientDecodeResult{}, err
	}
	model, ok := operation.Model.Get()
	if !ok || strings.TrimSpace(model) == "" {
		return wire.ClientDecodeResult{}, canonical.BadRequest("GenerateContent operation model is required")
	}
	streaming, ok := operation.Streaming.Get()
	if !ok {
		return wire.ClientDecodeResult{}, canonical.BadRequest("GenerateContent operation delivery is required")
	}
	params := canonical.RequestParams{Model: canonical.Specify(model), Items: items, Controls: controls, Reasoning: reasoning}
	if toolPolicy != nil {
		params.ToolPolicy = canonical.Specify(*toolPolicy)
	}
	if outputFormat != nil {
		params.OutputFormat = canonical.Specify(*outputFormat)
	}
	result := wire.ClientRequestResult{Request: canonical.NewCanonicalRequest(params)}
	history, err := fingerprintHistory(request.Contents)
	if err != nil {
		return wire.ClientDecodeResult{}, canonical.InternalError("GenerateContent history fingerprint failed")
	}
	result.RequestFingerprint = history.request
	if history.previous != nil {
		rebasedDTO := request
		rebasedDTO.Contents = history.current
		var rebasedChanges []compat.Change
		rebasedItems, decodeErr := decodeItemsView(rebasedDTO, decoder.ImageLimits, &rebasedChanges, false)
		if decodeErr != nil {
			return wire.ClientDecodeResult{}, decodeErr
		}
		rebasedParams := params
		rebasedParams.Items = rebasedItems
		result.RebasedRequest = &wire.RebasedRequest{Previous: *history.previous, Request: canonical.NewCanonicalRequest(rebasedParams)}
	}
	if streaming {
		result.Delivery = delivery.StreamingDelivery(delivery.FramingSSE)
	} else {
		result.Delivery = delivery.BufferedDelivery()
	}
	return wire.ClientDecodeResult{Request: result, Changes: changes}, nil
}

func decodeClosed(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return canonical.BadRequest("GenerateContent request is invalid: " + err.Error())
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return canonical.BadRequest("GenerateContent request contains trailing data")
	}
	return nil
}

func decodeItems(request requestDTO, imageLimits shared.ImageDecodeLimitPolicy, changes *[]compat.Change) ([]canonical.CanonicalItem, error) {
	return decodeItemsView(request, imageLimits, changes, true)
}

func decodeItemsView(request requestDTO, imageLimits shared.ImageDecodeLimitPolicy, changes *[]compat.Change, validateLocalCorrelation bool) ([]canonical.CanonicalItem, error) {
	var items []canonical.CanonicalItem
	if request.SystemInstruction != nil {
		system, err := decodeSystemInstruction(*request.SystemInstruction)
		if err != nil {
			return nil, err
		}
		items = append(items, system)
	}
	if len(request.Tools) > 0 {
		declaration, err := decodeTools(request.Tools)
		if err != nil {
			return nil, err
		}
		items = append(items, declaration)
	}
	var pending map[string]string
	if validateLocalCorrelation {
		pending = map[string]string{}
	}
	for _, content := range request.Contents {
		if pending != nil {
			responseCount := 0
			for _, part := range content.Parts {
				if part.FunctionResponse != nil {
					responseCount++
				}
			}
			if responseCount > 0 && responseCount != len(pending) {
				return nil, canonical.BadRequest("GenerateContent response step requires exactly one functionResponse per outstanding functionCall")
			}
		}
		decoded, _, err := decodeContentParts(content, pending, imageLimits, changes, uint32(len(items)))
		if err != nil {
			return nil, err
		}
		items = append(items, decoded...)
	}
	if err := shared.ValidateImageDecodeLimits(items, imageLimits); err != nil {
		return nil, canonical.BadRequest("GenerateContent images exceed request limits")
	}
	return items, nil
}

func decodeSystemInstruction(content contentDTO) (canonical.CanonicalItem, error) {
	if content.Role != "" && content.Role != "user" {
		return canonical.CanonicalItem{}, canonical.BadRequest("systemInstruction role must be user when specified")
	}
	if len(content.Parts) == 0 {
		return canonical.CanonicalItem{}, canonical.BadRequest("systemInstruction requires text parts")
	}
	parts := make([]canonical.MessagePart, 0, len(content.Parts))
	for _, part := range content.Parts {
		if part.Text == nil || part.Thought != nil || part.ThoughtSignature != nil || part.FunctionCall != nil || part.FunctionResponse != nil || part.InlineData != nil || part.FileData != nil {
			return canonical.CanonicalItem{}, canonical.BadRequest("systemInstruction supports text parts only")
		}
		parts = append(parts, canonical.NewTextMessagePart(*part.Text))
	}
	return canonical.NewScopedMessageItem(canonical.MessageRoleSystem, parts, canonical.ContextScopeRequest)
}

func decodeContentParts(content contentDTO, pending map[string]string, imageLimits shared.ImageDecodeLimitPolicy, changes *[]compat.Change, itemOffset uint32) ([]canonical.CanonicalItem, canonical.TurnOwner, error) {
	if len(content.Parts) == 0 {
		return nil, "", canonical.BadRequest("GenerateContent content requires parts")
	}
	role := content.Role
	if role == "" {
		role = "user"
	}
	var owner canonical.TurnOwner
	var items []canonical.CanonicalItem
	var messageParts []canonical.MessagePart
	var messageRole canonical.MessageRole
	flushMessage := func() error {
		if len(messageParts) == 0 {
			return nil
		}
		item, err := canonical.NewMessageItem(messageRole, messageParts)
		if err != nil {
			return err
		}
		items = append(items, item)
		messageParts = nil
		return nil
	}
	for _, part := range content.Parts {
		partOwner, classifyErr := classifyPart(role, part)
		if classifyErr != nil {
			return nil, "", classifyErr
		}
		var item canonical.CanonicalItem
		var err error
		switch {
		case part.Text != nil:
			if part.Thought != nil && *part.Thought {
				if err := flushMessage(); err != nil {
					return nil, "", err
				}
				partOwner = canonical.TurnOwnerAssistant
				reasoning, reasoningErr := canonical.NewReasoningPart(canonical.ReasoningPartSummary, *part.Text)
				if reasoningErr != nil {
					return nil, "", reasoningErr
				}
				item, err = canonical.NewReasoningItem([]canonical.ReasoningPart{reasoning}, canonical.OpaqueThinking{})
				break
			}
			messageRole = canonical.MessageRoleUser
			if role == "model" {
				messageRole = canonical.MessageRoleAssistant
				partOwner = canonical.TurnOwnerAssistant
			} else if role != "user" {
				return nil, "", canonical.BadRequest("GenerateContent content role is invalid")
			}
			messageParts = append(messageParts, canonical.NewTextMessagePart(*part.Text))
			item = canonical.CanonicalItem{}
		case part.FunctionCall != nil:
			if err := flushMessage(); err != nil {
				return nil, "", err
			}
			partOwner = canonical.TurnOwnerAssistant
			call := part.FunctionCall
			if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
				return nil, "", canonical.BadRequest("GenerateContent functionCall requires id and name")
			}
			id, idErr := canonical.NewToolCallID(call.ID)
			key, keyErr := canonical.NewRequestToolKey(canonical.ToolKindFunction, call.Name)
			object, objectErr := parseObjectDefault(call.Args)
			if idErr != nil || keyErr != nil || objectErr != nil {
				return nil, "", canonical.BadRequest("GenerateContent functionCall is invalid")
			}
			item, err = canonical.NewToolCallItem(id, key, canonical.NewJSONObjectToolInput(object))
			if pending != nil {
				if _, exists := pending[call.ID]; exists {
					return nil, "", canonical.BadRequest("GenerateContent functionCall id is duplicated; use a client that preserves unique call IDs")
				}
				pending[call.ID] = call.Name
			}
		case part.FunctionResponse != nil:
			if err := flushMessage(); err != nil {
				return nil, "", err
			}
			response := part.FunctionResponse
			if response.WillContinue != nil || response.Scheduling != nil {
				return nil, "", canonical.BadRequest("GenerateContent non-blocking function responses are unsupported")
			}
			if strings.TrimSpace(response.ID) == "" || strings.TrimSpace(response.Name) == "" {
				return nil, "", canonical.BadRequest("GenerateContent functionResponse requires id and name")
			}
			if pending != nil {
				name, exists := pending[response.ID]
				if !exists || name != response.Name {
					return nil, "", canonical.BadRequest("GenerateContent functionResponse does not match a pending call")
				}
				delete(pending, response.ID)
			}
			id, idErr := canonical.NewToolCallID(response.ID)
			if idErr != nil {
				return nil, "", canonical.BadRequest("GenerateContent functionResponse id is invalid")
			}
			text, isError, approximated, responseErr := responseText(response.Response)
			if responseErr != nil {
				return nil, "", responseErr
			}
			resultParts := []canonical.ToolResultPart{canonical.NewTextToolResultPart(text)}
			for _, media := range response.Parts {
				if media.InlineData == nil || media.FileData != nil {
					return nil, "", canonical.BadRequest("GenerateContent functionResponse media is unsupported")
				}
				image, imageErr := decodeInlineImage(*media.InlineData, imageLimits)
				if imageErr != nil {
					return nil, "", imageErr
				}
				resultParts = append(resultParts, canonical.NewImageToolResultPart(image))
			}
			item, err = canonical.NewToolResultItem(id, resultParts, isError)
			if approximated && changes != nil {
				*changes = compat.AppendUnique(*changes, compat.NewApproximation(canonical.RequestItemsToolResultContent, canonical.RequestItemOccurrence(itemOffset+uint32(len(items)))))
			}
		case part.InlineData != nil:
			image, imageErr := decodeInlineImage(*part.InlineData, imageLimits)
			if imageErr != nil {
				return nil, "", imageErr
			}
			messageRole = canonical.MessageRoleUser
			if role == "model" {
				messageRole = canonical.MessageRoleAssistant
				partOwner = canonical.TurnOwnerAssistant
			} else if role != "user" {
				return nil, "", canonical.BadRequest("GenerateContent content role is invalid")
			}
			messageParts = append(messageParts, canonical.NewImageMessagePart(image))
			item = canonical.CanonicalItem{}
		}
		if err != nil {
			return nil, "", err
		}
		if owner != "" && owner != partOwner {
			return nil, "", canonical.BadRequest("GenerateContent content mixes caller and assistant ownership")
		}
		owner = partOwner
		if part.ThoughtSignature != nil && *part.ThoughtSignature != "" && changes != nil {
			*changes = compat.AppendUnique(*changes, compat.NewOmission(canonical.RequestItemsReasoningReplay, canonical.RequestItemOccurrence(itemOffset+uint32(len(items)))))
		}
		if item.Kind() != "" {
			items = append(items, item)
		}
	}
	if err := flushMessage(); err != nil {
		return nil, "", err
	}
	return items, owner, nil
}

func classifyPart(role string, part partDTO) (canonical.TurnOwner, error) {
	branches := 0
	for _, present := range []bool{part.Text != nil, part.FunctionCall != nil, part.FunctionResponse != nil, part.InlineData != nil, part.FileData != nil} {
		if present {
			branches++
		}
	}
	if branches != 1 {
		return "", canonical.BadRequest("GenerateContent part must contain exactly one supported semantic branch")
	}
	if part.FileData != nil {
		return "", canonical.BadRequest("GenerateContent fileData is unsupported")
	}
	if part.Thought != nil && part.Text == nil {
		return "", canonical.BadRequest("GenerateContent thought metadata is supported on text parts only")
	}
	if role != "user" && role != "model" {
		return "", canonical.BadRequest("GenerateContent content role is invalid")
	}
	if part.FunctionCall != nil {
		return canonical.TurnOwnerAssistant, nil
	}
	if part.FunctionResponse != nil {
		return canonical.TurnOwnerUser, nil
	}
	if part.Thought != nil && *part.Thought {
		return canonical.TurnOwnerAssistant, nil
	}
	if role == "model" {
		return canonical.TurnOwnerAssistant, nil
	}
	return canonical.TurnOwnerUser, nil
}

func parseObjectDefault(raw json.RawMessage) (canonical.JSONObject, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return canonical.EmptyJSONObject(), nil
	}
	return canonical.ParseJSONObject(raw)
}
func responseText(raw json.RawMessage) (string, bool, bool, error) {
	object, err := canonical.ParseJSONObject(raw)
	if err != nil {
		return "", false, false, canonical.BadRequest("GenerateContent functionResponse.response must be an object")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(object.Bytes(), &fields)
	isError := fields["error"] != nil
	if len(fields) == 1 {
		for _, key := range []string{"output", "result", "error"} {
			if value := fields[key]; value != nil {
				var text string
				if json.Unmarshal(value, &text) == nil {
					return text, key == "error", false, nil
				}
			}
		}
	}
	return object.String(), isError, true, nil
}

func decodeInlineImage(data inlineDataDTO, limits shared.ImageDecodeLimitPolicy) (canonical.ImagePart, error) {
	mediaType, err := shared.NormalizeImageMediaType(data.MIMEType)
	if err != nil {
		return canonical.ImagePart{}, canonical.BadRequest("GenerateContent inlineData media type is unsupported")
	}
	raw, err := shared.DecodeBase64Limited(data.Data, limits.MaxInlineBytes)
	if err != nil {
		return canonical.ImagePart{}, canonical.BadRequest("GenerateContent inlineData is invalid")
	}
	image, err := canonical.NewInlineImage(mediaType, raw, canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		return canonical.ImagePart{}, canonical.BadRequest("GenerateContent inlineData is invalid")
	}
	return image, nil
}

func decodeTools(tools []toolDTO) (canonical.CanonicalItem, error) {
	var declarations []canonical.ToolDeclaration
	for _, tool := range tools {
		if tool.GoogleSearch != nil {
			var fields map[string]json.RawMessage
			if json.Unmarshal(*tool.GoogleSearch, &fields) != nil || fields == nil || len(fields) != 0 {
				return canonical.CanonicalItem{}, canonical.BadRequest("GenerateContent googleSearch options are unsupported")
			}
			declarations = append(declarations, canonical.NewWebSearchDeclaration())
		}
		if len(tool.FunctionDeclarations) == 0 && tool.GoogleSearch == nil {
			return canonical.CanonicalItem{}, canonical.BadRequest("GenerateContent tool is unsupported")
		}
		for _, function := range tool.FunctionDeclarations {
			if function.Parameters != nil || function.Response != nil || function.ResponseJSONSchema != nil {
				return canonical.CanonicalItem{}, canonical.BadRequest("GenerateContent function declaration schema combination is unsupported")
			}
			key, err := canonical.NewRequestToolKey(canonical.ToolKindFunction, function.Name)
			if err != nil {
				return canonical.CanonicalItem{}, err
			}
			schema, err := parseObjectDefault(function.ParametersJSONSchema)
			if err != nil {
				return canonical.CanonicalItem{}, canonical.BadRequest("GenerateContent function parameters must be an object schema")
			}
			declaration, err := canonical.NewFunctionTool(key, function.Description, canonical.NewToolSchemaObject(schema), canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
			if err != nil {
				return canonical.CanonicalItem{}, err
			}
			declarations = append(declarations, declaration)
		}
	}
	set, err := canonical.NewToolSet(declarations)
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	return canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
}

func decodeControls(config *generationConfig, changes *[]compat.Change) (canonical.GenerationControls, error) {
	if config == nil {
		return canonical.GenerationControls{}, nil
	}
	if config.CandidateCount != nil && *config.CandidateCount != 1 {
		return canonical.GenerationControls{}, canonical.BadRequest("GenerateContent candidateCount must be one")
	}
	if config.Seed != nil || config.PresencePenalty != nil || config.FrequencyPenalty != nil || config.ResponseLogprobs != nil || config.Logprobs != nil || config.ResponseSchema != nil {
		return canonical.GenerationControls{}, canonical.BadRequest("GenerateContent generationConfig contains unsupported semantics")
	}
	if config.TopK != nil {
		if *config.TopK <= 0 {
			return canonical.GenerationControls{}, canonical.BadRequest("GenerateContent topK must be greater than zero")
		}
		*changes = compat.AppendUnique(*changes, compat.NewOmission(canonical.RequestControlsTopK, canonical.Occurrence{}))
	}
	params := canonical.GenerationControlsParams{MaxOutputTokens: config.MaxOutputTokens, StopSequences: config.StopSequences, Temperature: config.Temperature, TopP: config.TopP}
	if config.ThinkingConfig != nil {
		level := strings.ToLower(strings.TrimSpace(config.ThinkingConfig.ThinkingLevel))
		if level != "" {
			effort := canonical.InferenceEffort(level)
			switch effort {
			case canonical.InferenceEffortMinimal, canonical.InferenceEffortLow, canonical.InferenceEffortMedium, canonical.InferenceEffortHigh:
				params.Effort = &effort
			default:
				return canonical.GenerationControls{}, canonical.BadRequest("GenerateContent thinkingLevel is unsupported")
			}
		}
	}
	return canonical.NewGenerationControls(params)
}

func decodeToolPolicy(config *toolConfigDTO) (*canonical.ToolPolicy, error) {
	if config == nil || config.FunctionCallingConfig == nil {
		return nil, nil
	}
	choice := config.FunctionCallingConfig
	mode := strings.ToUpper(strings.TrimSpace(choice.Mode))
	if len(choice.AllowedFunctionNames) > 1 {
		return nil, canonical.BadRequest("GenerateContent allowedFunctionNames cannot contain multiple functions")
	}
	var policy canonical.ToolPolicy
	switch mode {
	case "NONE":
		if len(choice.AllowedFunctionNames) != 0 {
			return nil, canonical.BadRequest("GenerateContent NONE cannot restrict functions")
		}
		policy = canonical.NewToolPolicy(canonical.ToolPolicyNone, nil)
	case "", "AUTO":
		if len(choice.AllowedFunctionNames) != 0 {
			return nil, canonical.BadRequest("GenerateContent AUTO subsets are unsupported")
		}
		policy = canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil)
	case "ANY":
		if len(choice.AllowedFunctionNames) == 0 {
			policy = canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil)
		} else {
			key, err := canonical.NewRequestToolKey(canonical.ToolKindFunction, choice.AllowedFunctionNames[0])
			if err != nil {
				return nil, err
			}
			policy = canonical.NewToolPolicy(canonical.ToolPolicySpecific, &key)
		}
	case "VALIDATED":
		return nil, canonical.BadRequest("GenerateContent VALIDATED tool mode is unsupported")
	default:
		return nil, canonical.BadRequest("GenerateContent function calling mode is invalid")
	}
	return &policy, nil
}

func decodeOutputFormat(config *generationConfig) (*canonical.OutputFormat, error) {
	if config == nil {
		return nil, nil
	}
	mime := strings.TrimSpace(config.ResponseMimeType)
	if config.ResponseJSONSchema != nil {
		if mime != "" && mime != "application/json" {
			return nil, canonical.BadRequest("GenerateContent responseJsonSchema requires application/json")
		}
		object, err := canonical.ParseJSONObject(config.ResponseJSONSchema)
		if err != nil {
			return nil, canonical.BadRequest("GenerateContent responseJsonSchema must be a JSON object")
		}
		format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONSchema, Name: "generate_content_response", Schema: canonical.NewRawJSONObject(object.String()), SchemaContract: canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled}})
		return &format, err
	}
	if mime == "" {
		return nil, nil
	}
	kind := canonical.OutputFormatText
	if mime == "application/json" {
		kind = canonical.OutputFormatJSONObject
	} else if mime != "text/plain" {
		return nil, canonical.BadRequest("GenerateContent responseMimeType is unsupported")
	}
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: kind})
	return &format, err
}

func decodeReasoning(config *generationConfig) (canonical.ReasoningControls, error) {
	if config == nil || config.ThinkingConfig == nil {
		return canonical.ReasoningControls{}, nil
	}
	thinking := config.ThinkingConfig
	if strings.TrimSpace(thinking.ThinkingLevel) != "" && thinking.ThinkingBudget != nil {
		return canonical.ReasoningControls{}, canonical.BadRequest("GenerateContent thinkingLevel and thinkingBudget cannot both be represented")
	}
	params := canonical.ReasoningControlsParams{}
	if thinking.ThinkingBudget != nil {
		var compute canonical.ReasoningCompute
		switch {
		case *thinking.ThinkingBudget == -1:
			compute = canonical.NewAutomaticReasoningCompute()
		case *thinking.ThinkingBudget == 0:
			compute = canonical.NewDisabledReasoningCompute()
		case *thinking.ThinkingBudget > 0:
			var err error
			compute, err = canonical.NewBudgetReasoningCompute(*thinking.ThinkingBudget)
			if err != nil {
				return canonical.ReasoningControls{}, err
			}
		default:
			return canonical.ReasoningControls{}, canonical.BadRequest("GenerateContent thinkingBudget must be -1, zero, or positive")
		}
		params.Compute = canonical.Specify(compute)
	}
	if thinking.IncludeThoughts != nil {
		disclosure := canonical.ReasoningDisclosureNone
		if *thinking.IncludeThoughts {
			disclosure = canonical.ReasoningDisclosureSummary
		}
		params.Disclosure = canonical.Specify(disclosure)
	}
	return canonical.NewReasoningControls(params)
}
