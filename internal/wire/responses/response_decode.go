// event-state machine together so migration behavior stays recoverable.
package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

type responseEnvelope struct {
	ID                string                         `json:"id"`
	Model             string                         `json:"model"`
	Status            string                         `json:"status"`
	OutputText        string                         `json:"output_text"`
	Output            []json.RawMessage              `json:"output"`
	IncompleteDetails *responsesIncompleteDetailsDTO `json:"incomplete_details,omitempty"`
	ContentFilters    []responsesContentFilterDTO    `json:"content_filters,omitempty"`
}

var tokenUsagePathSpec = core.TokenUsagePathSpec{
	InputPaths: [][]string{
		{"usage", "input_tokens"},
		{"usage", "prompt_tokens"},
		{"response", "usage", "input_tokens"},
		{"response", "usage", "prompt_tokens"},
		{"usageMetadata", "promptTokenCount"},
		{"usage", "inputTokens"},
		{"response", "usage", "inputTokens"},
	},
	OutputPaths: [][]string{
		{"usage", "output_tokens"},
		{"usage", "completion_tokens"},
		{"response", "usage", "output_tokens"},
		{"response", "usage", "completion_tokens"},
		{"usageMetadata", "candidatesTokenCount"},
		{"usage", "outputTokens"},
		{"response", "usage", "outputTokens"},
	},
	ReasoningPaths: [][]string{
		{"usage", "output_tokens_details", "reasoning_tokens"},
		{"response", "usage", "output_tokens_details", "reasoning_tokens"},
	},
	CacheReadPaths: [][]string{
		{"usage", "input_tokens_details", "cached_tokens"},
		{"usage", "prompt_tokens_details", "cached_tokens"},
		{"response", "usage", "input_tokens_details", "cached_tokens"},
		{"response", "usage", "prompt_tokens_details", "cached_tokens"},
		{"usage", "cache_read_input_tokens"},
		{"response", "usage", "cache_read_input_tokens"},
		{"usageMetadata", "cachedContentTokenCount"},
		{"usage", "cacheReadInputTokens"},
		{"response", "usage", "cacheReadInputTokens"},
	},
	CacheWritePaths: [][]string{
		{"usage", "input_tokens_details", "cache_write_tokens"},
		{"usage", "prompt_tokens_details", "cache_write_tokens"},
		{"response", "usage", "input_tokens_details", "cache_write_tokens"},
		{"response", "usage", "prompt_tokens_details", "cache_write_tokens"},
		{"usage", "cache_creation_input_tokens"},
		{"response", "usage", "cache_creation_input_tokens"},
		{"usage", "cacheWriteInputTokens"},
		{"response", "usage", "cacheWriteInputTokens"},
	},
}

func decodeResponseBuffered(ctx context.Context, request canonical.CanonicalRequest, raw []byte, exchangeID string, sink compat.Sink) (canonical.ResponseStream, error) {
	var dto responseEnvelope
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, canonical.InternalError("responses output is invalid JSON")
	}
	if strings.TrimSpace(dto.ID) == "" { // swobu:io-string source=provider-wire
		return nil, canonical.InternalError("responses output is missing id")
	}
	usage := core.ExtractTokenUsage(raw, tokenUsagePathSpec)
	_, inputPresent := usage.InputTokens()
	openaiwire.EmitUsageDecision(ctx, sink, exchangeID, inputPresent, compat.ResponseUsageInputTokens, compat.Subject("wire:/usage/input_tokens"))
	_, outputPresent := usage.OutputTokens()
	openaiwire.EmitUsageDecision(ctx, sink, exchangeID, outputPresent, compat.ResponseUsageOutputTokens, compat.Subject("wire:/usage/output_tokens"))
	_, reasoningPresent := usage.ReasoningTokens()
	openaiwire.EmitUsageDecision(ctx, sink, exchangeID, reasoningPresent, compat.ResponseUsageReasoningTokens, compat.Subject("wire:/usage/output_tokens_details/reasoning_tokens"))
	_, cacheReadPresent := usage.CacheReadTokens()
	openaiwire.EmitUsageDecision(ctx, sink, exchangeID, cacheReadPresent, compat.ResponseUsageCacheReadTokens, compat.Subject("wire:/usage/cache_read_tokens"))
	_, cacheWritePresent := usage.CacheWriteTokens()
	openaiwire.EmitUsageDecision(ctx, sink, exchangeID, cacheWritePresent, compat.ResponseUsageCacheWriteTokens, compat.Subject("wire:/usage/cache_write_tokens"))
	if terminalReason, promptBlocked := responsesTerminalReason("", dto.Status, "", dto.ContentFilters, responseIncompleteReason(dto.IncompleteDetails)); promptBlocked {
		message := responsesContentFilterMessage(responsesBlockedContentFilterSource(dto.ContentFilters))
		return nil, canonical.NewBackendError("responses", http.StatusForbidden, message, "")
	} else {
		responseStatus := responsesTerminalStatus("", dto.Status, "")
		if err := admitResponsesProjectableResponseStatus(responseStatus); err != nil {
			return nil, err
		}
		items, err := decodeOutputItemsForResponse(ctx, request, dto.Output, dto.OutputText, responseStatus, exchangeID, sink)
		if err != nil {
			return nil, err
		}
		emitNativeResponseIDCaptured(ctx, sink, exchangeID, dto.ID)
		return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
			exchangeID,
			canonical.ResponseRef{Responses: &canonical.ResponsesContinuation{ProviderResponseID: canonical.NewResponsesResponseID(dto.ID)}},
			dto.Model,
			items,
			responsesCompletion(responseStatus, terminalReason),
			usage,
		)), nil
	}
}

func admitResponsesProjectableResponseStatus(status string) error {
	switch strings.TrimSpace(status) { // swobu:io-string source=provider-wire
	case "completed", "incomplete":
		return nil
	default:
		return canonical.NewBackendError("responses", 0, "responses terminal status cannot carry successful canonical output", "")
	}
}

func emitNativeResponseIDCaptured(ctx context.Context, sink compat.Sink, exchangeID string, providerResponseID string) {
	if sink == nil || strings.TrimSpace(providerResponseID) == "" { // swobu:io-string source=provider-wire
		return
	}
	_ = sink.Commit(ctx, exchangeID, []compat.Decision{{
		Feature: compat.ResponseIDResponses,
		Outcome: compat.Exact,
		Subject: compat.Subject("wire:/id"),
	}})
}

func decodeOutputItems(ctx context.Context, request canonical.CanonicalRequest, wireItems any, outputText string, exchangeID string, sink compat.Sink) ([]canonical.CanonicalItem, error) {
	return decodeCompletedResponsesItemSetForResponse(ctx, request, wireItems, outputText, "completed", exchangeID, sink)
}

func decodeOutputItemsForResponse(ctx context.Context, request canonical.CanonicalRequest, wireItems any, outputText string, responseStatus string, exchangeID string, sink compat.Sink) ([]canonical.CanonicalItem, error) {
	return decodeCompletedResponsesItemSetForResponse(ctx, request, wireItems, outputText, responseStatus, exchangeID, sink)
}

func decodeCompletedResponsesItemSet(ctx context.Context, request canonical.CanonicalRequest, wireItems any, outputText string, exchangeID string, sink compat.Sink) ([]canonical.CanonicalItem, error) {
	return decodeCompletedResponsesItemSetForResponse(ctx, request, wireItems, outputText, "completed", exchangeID, sink)
}

func decodeCompletedResponsesItemSetForResponse(ctx context.Context, request canonical.CanonicalRequest, wireItems any, outputText string, responseStatus string, exchangeID string, sink compat.Sink) ([]canonical.CanonicalItem, error) {
	return decodeCompletedResponsesItemSetAtIndexes(ctx, request, wireItems, outputText, nil, false, responseStatus, exchangeID, sink)
}

func decodeCompletedResponsesItemSetAtIndexes(
	ctx context.Context,
	request canonical.CanonicalRequest,
	wireItems any,
	outputText string,
	originalIndexes []int,
	survivingOutput bool,
	responseStatus string,
	exchangeID string,
	sink compat.Sink,
) ([]canonical.CanonicalItem, error) {
	items, err := rawResponsesOutputItems(wireItems)
	if err != nil {
		return nil, err
	}
	if originalIndexes != nil && len(originalIndexes) != len(items) {
		return nil, canonical.InternalError("responses output indexes do not match output items")
	}
	environment, err := canonical.EffectiveTools(request)
	if err != nil {
		return nil, canonical.InternalError("responses tool environment is ambiguous")
	}
	tools := environment.Declarations()
	output := make([]canonical.CanonicalItem, 0, len(items))
	erasedSemantic := false
	for position, rawItem := range items {
		index := position
		if originalIndexes != nil {
			index = originalIndexes[position]
		}
		var item responsesWireOutputItemDTO
		if err := json.Unmarshal(rawItem, &item); err != nil {
			return nil, canonical.InternalError("responses output item is invalid JSON")
		}
		admission, err := admitCompletedResponsesOutputItem(item, responseStatus)
		if err != nil {
			return nil, err
		}
		itemType := admission.itemType
		if admission.disposition == responsesOutputErase {
			erasedSemantic = true
			if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.ResponseItemsKind, compat.Drop, compat.Subject(fmt.Sprintf("wire:/output/%d/%s", index, admission.eraseField))); err != nil {
				return nil, err
			}
			continue
		}
		switch itemType {
		case "message":
			message, present, err := decodeResponsesMessageOutputItem(item, sink, exchangeID, fmt.Sprintf("wire:/output/%d/content", index))
			if err != nil {
				return nil, err
			}
			if present {
				output = append(output, message)
			} else if len(bytes.TrimSpace(item.Content)) > 0 && !bytes.Equal(bytes.TrimSpace(item.Content), []byte("null")) {
				erasedSemantic = true
			}
		case "function_call":
			object, err := decodeResponsesFunctionCallArguments(item.Arguments)
			if err != nil {
				return nil, canonical.InternalError("responses tool call arguments are invalid")
			}
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			name := strings.TrimSpace(item.Name)     // swobu:io-string source=boundary
			if callID == "" {
				return nil, canonical.InternalError("responses tool call is missing call_id")
			}
			resolved, _, err := canonical.ResolveToolDeclarationByName(tools, name, canonical.ToolTypeFunction)
			if err != nil {
				return nil, canonical.InternalError("responses tool call references an unknown or ambiguous tool")
			}
			canonicalCallID, _ := canonical.NewToolCallID(callID)
			call, err := canonical.NewToolCallItem(canonicalCallID, resolved.Key(), canonical.NewJSONObjectToolInput(object))
			if err != nil {
				return nil, canonical.InternalError("responses tool call is invalid")
			}
			output = append(output, call)
		case "tool_search_call":
			execution := strings.TrimSpace(item.Execution)
			executor, ok := decodeResponsesToolExecutor(execution)
			if !ok {
				return nil, canonical.InternalError("responses tool discovery call has invalid execution")
			}
			if declaration, ok := environment.Lookup(canonical.ToolDiscoveryKey()); !ok || declaration.Kind() != canonical.ToolKindDiscovery {
				return nil, canonical.InternalError("responses tool discovery call was not available to the provider attempt")
			}
			callID, err := canonical.NewToolCallID(strings.TrimSpace(item.CallID))
			if err != nil {
				return nil, canonical.InternalError("responses tool discovery call is missing call_id")
			}
			arguments, err := decodeResponsesFunctionCallArguments(item.Arguments)
			if err != nil {
				return nil, canonical.InternalError("responses tool discovery call arguments are invalid")
			}
			call, err := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(arguments), executor)
			if err != nil {
				return nil, canonical.InternalError("responses tool discovery call is invalid")
			}
			output = append(output, call)
		case "tool_search_output":
			executor, ok := decodeResponsesToolExecutor(item.Execution)
			if !ok {
				return nil, canonical.InternalError("responses tool discovery output has invalid execution")
			}
			callID, err := canonical.NewToolCallID(strings.TrimSpace(item.CallID))
			if err != nil {
				return nil, canonical.InternalError("responses tool discovery output is missing call_id")
			}
			projected, err := decodeResponsesProviderAdditionalTools(item.Tools, fmt.Sprintf("wire:/output/%d/tools", index), sink, exchangeID)
			if err != nil {
				return nil, err
			}
			if projected.allErased() {
				return nil, canonical.NewBackendError("responses", 0, "responses tool discovery output has no surviving declarations", "")
			}
			set, err := canonical.NewToolSet(projected.declarations)
			if err != nil {
				return nil, canonical.InternalError("responses tool discovery output tools are ambiguous")
			}
			result, err := canonical.NewToolDiscoveryResultItem(callID, set, executor)
			if err != nil {
				return nil, canonical.InternalError("responses tool discovery output is invalid")
			}
			output = append(output, result)
		case "custom_tool_call":
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			name := strings.TrimSpace(item.Name)     // swobu:io-string source=boundary
			if callID == "" {
				return nil, canonical.InternalError("responses custom tool call is missing call_id")
			}
			resolved, _, err := canonical.ResolveToolDeclarationByName(tools, name, canonical.ToolTypeCustom)
			if err != nil {
				return nil, canonical.InternalError("responses custom tool call references an unknown or ambiguous tool")
			}
			canonicalCallID, _ := canonical.NewToolCallID(callID)
			call, err := canonical.NewToolCallItem(canonicalCallID, resolved.Key(), canonical.NewTextToolInput(item.Input))
			if err != nil {
				return nil, canonical.InternalError("responses custom tool call is invalid")
			}
			output = append(output, call)
		case "web_search_call":
			rawAction := bytes.TrimSpace(item.Action)
			if len(rawAction) == 0 || bytes.Equal(rawAction, []byte("null")) {
				if strings.TrimSpace(item.Status) != "completed" { // swobu:io-string source=provider-wire
					return nil, canonical.NewBackendError("responses", 0, "responses actionless web-search marker is not completed", "")
				}
				// The marker has no visible-output or continuation consumer.
				break
			}
			state, err := decodeResponsesWebSearchLifecycleState(item.Status)
			if err != nil {
				if err := emitResponsesCompatibilityDecision(
					sink,
					exchangeID,
					compat.ResponseItemsKind,
					compat.Drop,
					compat.Subject(fmt.Sprintf("wire:/output/%d/status", index)),
				); err != nil {
					return nil, err
				}
				state = responsesWebSearchUnknown
			}
			lifecycle, err := decodeResponsesWebSearchLifecycleWithDecisions(item.ID, item.Action, state, sink, exchangeID, fmt.Sprintf("wire:/output/%d/action/sources", index), true)
			if err != nil {
				return nil, err
			}
			output = append(output, lifecycle...)
		case "reasoning":
			reasoning, present, err := decodeResponsesReasoningItem(item, sink, exchangeID, compat.ResponseItemsKind, fmt.Sprintf("wire:/output/%d", index), true)
			if err != nil {
				return nil, err
			}
			if present {
				output = append(output, reasoning)
			} else if len(item.Summary) > 0 || len(bytes.TrimSpace(item.Content)) > 0 && !bytes.Equal(bytes.TrimSpace(item.Content), []byte("null")) {
				erasedSemantic = true
			}
		default:
			return nil, canonical.InternalError("responses admitted output disposition has no projector")
		}
	}
	if len(output) == 0 && strings.TrimSpace(outputText) != "" { // swobu:io-string source=boundary
		message, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart(outputText)})
		if err != nil {
			return nil, canonical.InternalError("responses output text is invalid")
		}
		output = append(output, message)
	}
	if erasedSemantic && len(output) == 0 && !survivingOutput {
		return nil, canonical.NewBackendError("responses", 0, "responses output has no surviving semantic items", "")
	}
	return output, nil
}

type responsesOutputDisposition uint8

const (
	responsesOutputProject responsesOutputDisposition = iota + 1
	responsesOutputDeferPartial
	responsesOutputErase
)

type responsesOutputAdmission struct {
	itemType    string
	disposition responsesOutputDisposition
	eraseField  string
}

// admitCompletedResponsesOutputItem composes provider item kind/status with the
// enclosing response contract. An empty responseStatus means output_item.done
// arrived before the response terminal and partial text/reasoning must defer.
func admitCompletedResponsesOutputItem(item responsesWireOutputItemDTO, responseStatus string) (responsesOutputAdmission, error) {
	itemType := strings.TrimSpace(item.Type) // swobu:io-string source=provider-wire
	if itemType == "" {
		return responsesOutputAdmission{}, canonical.NewBackendError("responses", 0, "responses output item is missing type", "")
	}
	if !responsesRecognizesWireOutputKind(itemType) {
		return responsesOutputAdmission{itemType: itemType, disposition: responsesOutputErase, eraseField: "type"}, nil
	}
	itemStatus := strings.TrimSpace(item.Status) // swobu:io-string source=provider-wire
	if itemStatus == "" {
		itemStatus = "completed"
	}
	enclosingStatus := strings.TrimSpace(responseStatus) // swobu:io-string source=provider-wire
	switch itemType {
	case "mcp_call":
		return responsesOutputAdmission{}, responsesProviderMCPContradiction()
	case "message", "reasoning":
		switch itemStatus {
		case "completed":
			return responsesOutputAdmission{itemType: itemType, disposition: responsesOutputProject}, nil
		case "incomplete":
			switch enclosingStatus {
			case "":
				return responsesOutputAdmission{itemType: itemType, disposition: responsesOutputDeferPartial}, nil
			case "incomplete":
				return responsesOutputAdmission{itemType: itemType, disposition: responsesOutputProject}, nil
			}
		}
	case "web_search_call":
		switch itemStatus {
		case "completed", "failed":
			return responsesOutputAdmission{itemType: itemType, disposition: responsesOutputProject}, nil
		case "incomplete":
			if enclosingStatus == "" {
				return responsesOutputAdmission{itemType: itemType, disposition: responsesOutputDeferPartial}, nil
			}
			if enclosingStatus == "incomplete" {
				return responsesOutputAdmission{itemType: itemType, disposition: responsesOutputProject}, nil
			}
			return responsesOutputAdmission{}, canonical.NewBackendError("responses", 0, "incomplete web-search output requires an incomplete response", "")
		default:
			return responsesOutputAdmission{itemType: itemType, disposition: responsesOutputProject}, nil
		}
	case "function_call", "custom_tool_call", "tool_search_call", "tool_search_output":
		if itemStatus == "completed" {
			return responsesOutputAdmission{itemType: itemType, disposition: responsesOutputProject}, nil
		}
	}
	return responsesOutputAdmission{}, canonical.NewBackendError("responses", 0, "responses output item status is inconsistent with its semantic kind and response", "")
}

func decodeResponsesProviderAdditionalTools(raw json.RawMessage, subjectPrefix string, sink compat.Sink, exchangeID string) (responsesAdditionalToolsProjection, error) {
	projected, err := decodeResponsesAdditionalTools(raw, subjectPrefix, compat.ResponseItemsKind, sink, exchangeID)
	if err == nil {
		return projected, nil
	}
	var wireErr canonical.Error
	if errors.As(err, &wireErr) && wireErr.Code == canonical.ErrorCodeBadRequest {
		message := strings.Replace(wireErr.Message, "responses request ", "responses provider ", 1)
		return responsesAdditionalToolsProjection{}, canonical.NewBackendError("responses", 0, message, "")
	}
	return responsesAdditionalToolsProjection{}, err
}

func admitResponsesProviderOutputChild(kind string) error {
	if strings.TrimSpace(kind) == "" {
		return canonical.NewBackendError("responses", 0, "responses output child is missing type", "")
	}
	return nil
}

func rawResponsesOutputItems(items any) ([]json.RawMessage, error) {
	switch values := items.(type) {
	case []json.RawMessage:
		return values, nil
	case []responsesWireOutputItemDTO:
		raw := make([]json.RawMessage, len(values))
		for index, item := range values {
			encoded, err := json.Marshal(item)
			if err != nil {
				return nil, canonical.InternalError("responses output item could not be decoded")
			}
			raw[index] = encoded
		}
		return raw, nil
	default:
		return nil, canonical.InternalError("responses output items have an invalid shape")
	}
}

func decodeResponsesMessageOutputItem(item responsesWireOutputItemDTO, sink compat.Sink, exchangeID string, subjectPrefix string) (canonical.CanonicalItem, bool, error) {
	parts, err := openaiwire.DecodeContentParts(item.Content, "responses message content is invalid")
	if err != nil {
		return canonical.CanonicalItem{}, false, canonical.InternalError("responses message content is invalid")
	}
	content := make([]canonical.MessagePart, 0, len(parts))
	err = openaiwire.WalkContentParts(parts, func(index int, part openaiwire.ContentPartItem) error {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=boundary
		if err := admitResponsesProviderOutputChild(partType); err != nil {
			return err
		}
		switch partType {
		case "text", "output_text", "input_text":
			citations, err := decodeResponsesAnnotations(part.Text, part.Annotations, sink, exchangeID, fmt.Sprintf("%s/%d/annotations", subjectPrefix, index))
			if err != nil {
				return err
			}
			messagePart, err := canonical.NewCitedTextMessagePart(part.Text, citations)
			if err != nil {
				return canonical.InternalError("responses output URL citations are invalid")
			}
			content = append(content, messagePart)
		default:
			return emitResponsesCompatibilityDecision(sink, exchangeID, compat.ResponseItemsKind, compat.Drop, compat.Subject(fmt.Sprintf("%s/%d/type", subjectPrefix, index)))
		}
		return nil
	})
	if err != nil {
		return canonical.CanonicalItem{}, false, err
	}
	if len(content) == 0 {
		return canonical.CanonicalItem{}, false, nil
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, content)
	if err != nil {
		return canonical.CanonicalItem{}, false, canonical.InternalError("responses message item is invalid")
	}
	return message, true, nil
}

// swobu:lint ignore string-switch because=Responses provider-wire summary types select canonical reasoning part kinds.
func decodeResponsesReasoningItem(item responsesWireOutputItemDTO, sink compat.Sink, exchangeID string, feature compat.Feature, subjectPrefix string, providerOutput bool) (canonical.CanonicalItem, bool, error) {
	content, err := decodeResponsesReasoningContent(item.Content)
	if err != nil {
		return canonical.CanonicalItem{}, false, err
	}
	parts := make([]canonical.ReasoningPart, 0, len(item.Summary)+len(content))
	for index, summary := range item.Summary {
		summaryType := strings.TrimSpace(summary.Type) // swobu:io-string source=provider-wire
		if providerOutput {
			if err := admitResponsesProviderOutputChild(summaryType); err != nil {
				return canonical.CanonicalItem{}, false, err
			}
		}
		if summaryType != "summary_text" {
			if err := emitResponsesCompatibilityDecision(sink, exchangeID, feature, compat.Drop, compat.Subject(fmt.Sprintf("%s/summary/%d/type", subjectPrefix, index))); err != nil {
				return canonical.CanonicalItem{}, false, err
			}
			continue
		}
		part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, summary.Text)
		if err != nil {
			return canonical.CanonicalItem{}, false, canonical.InternalError("responses reasoning part is invalid")
		}
		parts = append(parts, part)
	}
	for index, trace := range content {
		traceType := strings.TrimSpace(trace.Type) // swobu:io-string source=provider-wire
		if providerOutput {
			if err := admitResponsesProviderOutputChild(traceType); err != nil {
				return canonical.CanonicalItem{}, false, err
			}
		}
		if traceType != "reasoning_text" {
			if err := emitResponsesCompatibilityDecision(sink, exchangeID, feature, compat.Drop, compat.Subject(fmt.Sprintf("%s/content/%d/type", subjectPrefix, index))); err != nil {
				return canonical.CanonicalItem{}, false, err
			}
			continue
		}
		part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, trace.Text)
		if err != nil {
			return canonical.CanonicalItem{}, false, canonical.InternalError("responses reasoning trace is invalid")
		}
		parts = append(parts, part)
	}
	var opaque canonical.OpaqueThinking
	if item.EncryptedContent != "" {
		opaque, err = canonical.NewResponsesOpaqueThinking(canonical.ResponsesReasoningReplay{EncryptedContent: item.EncryptedContent})
		if err != nil {
			return canonical.CanonicalItem{}, false, canonical.InternalError("responses encrypted reasoning is invalid")
		}
	}
	if len(parts) == 0 && opaque.IsZero() {
		return canonical.CanonicalItem{}, false, nil
	}
	reasoning, err := canonical.NewReasoningItem(parts, opaque)
	if err != nil {
		return canonical.CanonicalItem{}, false, canonical.InternalError("responses reasoning item is invalid")
	}
	return reasoning, true, nil
}

func decodeResponsesReasoningContent(raw json.RawMessage) ([]responsesReasoningTextDTO, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var content []responsesReasoningTextDTO
	if err := json.Unmarshal(trimmed, &content); err != nil {
		return nil, canonical.InternalError("responses reasoning content is invalid")
	}
	return content, nil
}
