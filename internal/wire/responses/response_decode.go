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
	"github.com/swobuforge/swobu/internal/domain/responsesnative"
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
			canonical.ResponseRef{Responses: &canonical.ResponsesNativeRef{ProviderResponseID: canonical.NewResponsesNativeResponseID(dto.ID)}},
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
			rawArgs := item.Arguments
			object, err := canonical.ParseJSONObject([]byte(rawArgs))
			if err != nil {
				return nil, canonical.InternalError("responses tool call arguments are invalid")
			}
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			name := strings.TrimSpace(item.Name)     // swobu:io-string source=boundary
			if callID == "" {
				return nil, canonical.InternalError("responses tool call is missing call_id")
			}
			resolved, _, err := canonical.ResolveToolDeclarationByName(request.Tools(), name, canonical.ToolTypeFunction)
			if err != nil {
				return nil, canonical.InternalError("responses tool call references an unknown or ambiguous tool")
			}
			canonicalCallID, _ := canonical.NewToolCallID(callID)
			call, err := canonical.NewToolCallItem(canonicalCallID, resolved.Key(), canonical.NewJSONObjectToolInput(object))
			if err != nil {
				return nil, canonical.InternalError("responses tool call is invalid")
			}
			output = append(output, call)
		case "custom_tool_call":
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			name := strings.TrimSpace(item.Name)     // swobu:io-string source=boundary
			if callID == "" {
				return nil, canonical.InternalError("responses custom tool call is missing call_id")
			}
			resolved, _, err := canonical.ResolveToolDeclarationByName(request.Tools(), name, canonical.ToolTypeCustom)
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
			lifecycle, err := decodeResponsesWebSearchLifecycle(item.ID, item.Action, strings.TrimSpace(item.Status) == "completed") // swobu:io-string source=provider-wire
			if err != nil {
				return nil, err
			}
			output = append(output, lifecycle...)
		case "mcp_call":
			// The native batch retains this complete item even though portable
			// canonical has no P0 projection for it.
		case "reasoning":
			reasoning, present, err := decodeResponsesReasoningItem(item)
			if err != nil {
				return nil, err
			}
			if present {
				output = append(output, reasoning)
			}
		default:
			// Future native items remain exact continuation truth without being
			// assigned speculative portable semantics.
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
			return canonical.UnsupportedOperation("responses output item content part type is not implemented")
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
			return canonical.CanonicalItem{}, false, canonical.UnsupportedOperation("responses reasoning summary part type is not implemented")
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
	if len(parts) == 0 {
		return canonical.CanonicalItem{}, false, nil
	}
	reasoning, err := canonical.NewReasoningItem(parts, canonical.OpaqueThinking{})
	if err != nil {
		return canonical.CanonicalItem{}, false, canonical.InternalError("responses reasoning item is invalid")
	}
	return reasoning, true, nil
}

func captureBufferedResponsesOutput(raw []byte) (responsesnative.Items, error) {
	var envelope struct {
		Output []json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return responsesnative.Items{}, canonical.InternalError("responses output is invalid JSON")
	}
	items := make([][]byte, len(envelope.Output))
	for index := range envelope.Output {
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(envelope.Output[index], &header); err != nil || strings.TrimSpace(header.Type) == "" { // swobu:io-string source=provider-wire
			return responsesnative.Items{}, canonical.InternalError("responses output contains a malformed native item")
		}
		items[index] = envelope.Output[index]
	}
	batch, err := responsesnative.NewItems(items)
	if err != nil {
		return responsesnative.Items{}, canonical.InternalError("responses output contains an invalid native item")
	}
	return batch, nil
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
