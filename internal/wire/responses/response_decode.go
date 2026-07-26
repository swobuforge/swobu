// event-state machine together so migration behavior stays recoverable.
package responses

import (
	"bytes"
	"context"
	"encoding/json"
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
		items, err := decodeOutputItems(ctx, request, dto.Output, dto.OutputText, exchangeID, sink)
		if err != nil {
			return nil, err
		}
		emitNativeResponseIDCaptured(ctx, sink, exchangeID, dto.ID)
		return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
			exchangeID,
			canonical.ResponseRef{Responses: &canonical.ResponsesContinuation{ProviderResponseID: canonical.NewResponsesResponseID(dto.ID)}},
			dto.Model,
			items,
			terminalReason,
			usage,
		)), nil
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
	items, err := rawResponsesOutputItems(wireItems)
	if err != nil {
		return nil, err
	}
	output := make([]canonical.CanonicalItem, 0, len(items))
	for _, raw := range items {
		decoded, err := decodeCompletedResponsesItem(request, raw)
		if err != nil {
			return nil, err
		}
		output = append(output, decoded...)
	}
	if len(output) == 0 && strings.TrimSpace(outputText) != "" { // swobu:io-string source=boundary
		message, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart(outputText)})
		if err != nil {
			return nil, canonical.InternalError("responses output text is invalid")
		}
		output = append(output, message)
	}
	return output, nil
}

// decodeCompletedResponsesItem is the sole complete Responses item switch for
// buffered output and terminal streaming objects.
func decodeCompletedResponsesItem(request canonical.CanonicalRequest, raw json.RawMessage) ([]canonical.CanonicalItem, error) {
	return decodeCompletedResponsesItemSet(context.Background(), request, []json.RawMessage{raw}, "", "", nil)
}

func decodeCompletedResponsesItemSet(ctx context.Context, request canonical.CanonicalRequest, wireItems any, outputText string, exchangeID string, sink compat.Sink) ([]canonical.CanonicalItem, error) {
	items, err := rawResponsesOutputItems(wireItems)
	if err != nil {
		return nil, err
	}
	environment, err := canonical.EffectiveTools(request)
	if err != nil {
		return nil, canonical.InternalError("responses tool environment is ambiguous")
	}
	tools := environment.Declarations()
	output := make([]canonical.CanonicalItem, 0, len(items))
	for _, rawItem := range items {
		var item responsesWireOutputItemDTO
		if err := json.Unmarshal(rawItem, &item); err != nil {
			return nil, canonical.InternalError("responses output item is invalid JSON")
		}
		itemType := strings.TrimSpace(item.Type) // swobu:io-string source=provider-wire
		switch itemType {
		case "message":
			message, err := decodeResponsesMessageOutputItem(item)
			if err != nil {
				return nil, err
			}
			output = append(output, message)
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
			loaded, err := decodeResponsesAdditionalTools(item.Tools, nil, "")
			if err != nil {
				return nil, canonical.InternalError("responses tool discovery output tools are invalid")
			}
			set, err := canonical.NewToolSet(loaded)
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
					return nil, canonical.InternalError("responses actionless web-search marker is not completed")
				}
				// The marker has no visible-output or continuation consumer.
				break
			}
			state, err := decodeResponsesWebSearchLifecycleState(item.Status)
			if err != nil {
				return nil, canonical.NotImplemented("Swobu cannot project this valid Responses web-search history state")
			}
			lifecycle, err := decodeResponsesWebSearchLifecycle(item.ID, item.Action, state)
			if err != nil {
				return nil, err
			}
			output = append(output, lifecycle...)
		case "mcp_call":
			// No demonstrated client-output or continuation consumer.
		case "reasoning":
			reasoning, present, err := decodeResponsesReasoningItem(item)
			if err != nil {
				return nil, err
			}
			if present {
				output = append(output, reasoning)
			}
		default:
			// Unknown provider output is ignored until a named behavioral
			// consumer justifies a narrowly typed canonical representation.
		}
	}
	if len(output) == 0 && strings.TrimSpace(outputText) != "" { // swobu:io-string source=boundary
		message, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart(outputText)})
		if err != nil {
			return nil, canonical.InternalError("responses output text is invalid")
		}
		output = append(output, message)
	}
	return output, nil
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

func decodeResponsesMessageOutputItem(item responsesWireOutputItemDTO) (canonical.CanonicalItem, error) {
	parts, err := openaiwire.DecodeContentParts(item.Content, "responses message content is invalid")
	if err != nil {
		return canonical.CanonicalItem{}, canonical.InternalError("responses message content is invalid")
	}
	content := make([]canonical.MessagePart, 0, len(parts))
	err = openaiwire.WalkContentParts(parts, func(_ int, part openaiwire.ContentPartItem) error {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=boundary
		switch partType {
		case "text", "output_text", "input_text":
			citations, err := decodeResponsesAnnotations(part.Text, part.Annotations)
			if err != nil {
				return err
			}
			messagePart, err := canonical.NewCitedTextMessagePart(part.Text, citations)
			if err != nil {
				return canonical.InternalError("responses output URL citations are invalid")
			}
			content = append(content, messagePart)
		default:
			return canonical.NotImplemented("Swobu has no canonical projection for this Responses output content part type")
		}
		return nil
	})
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, content)
	if err != nil {
		return canonical.CanonicalItem{}, canonical.InternalError("responses message item is invalid")
	}
	return message, nil
}

// swobu:lint ignore string-switch because=Responses provider-wire summary types select canonical reasoning part kinds.
func decodeResponsesReasoningItem(item responsesWireOutputItemDTO) (canonical.CanonicalItem, bool, error) {
	content, err := decodeResponsesReasoningContent(item.Content)
	if err != nil {
		return canonical.CanonicalItem{}, false, err
	}
	parts := make([]canonical.ReasoningPart, 0, len(item.Summary)+len(content))
	for _, summary := range item.Summary {
		if strings.TrimSpace(summary.Type) != "summary_text" { // swobu:io-string source=provider-wire
			return canonical.CanonicalItem{}, false, canonical.NotImplemented("Swobu has no canonical projection for this Responses reasoning summary part type")
		}
		part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, summary.Text)
		if err != nil {
			return canonical.CanonicalItem{}, false, canonical.InternalError("responses reasoning part is invalid")
		}
		parts = append(parts, part)
	}
	for _, trace := range content {
		if strings.TrimSpace(trace.Type) != "reasoning_text" { // swobu:io-string source=provider-wire
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
