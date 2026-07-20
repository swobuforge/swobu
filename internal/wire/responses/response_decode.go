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
	Output            []responsesWireOutputItemDTO   `json:"output"`
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

func decodeOutputItems(ctx context.Context, request canonical.CanonicalRequest, items []responsesWireOutputItemDTO, outputText string, exchangeID string, sink compat.Sink) ([]canonical.CanonicalItem, error) {
	output := make([]canonical.CanonicalItem, 0, len(items))
	for _, item := range items {
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
		case "mcp_call":
			return nil, canonical.UnsupportedOperation("responses MCP output is not implemented")
		case "reasoning":
			reasoning, present, err := decodeResponsesReasoningItem(item)
			if err != nil {
				return nil, err
			}
			if present {
				output = append(output, reasoning)
			}
		default:
			return nil, canonical.UnsupportedOperation("responses output item type is not implemented")
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
			content = append(content, canonical.NewTextMessagePart(part.Text))
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
	if len(content) > 0 || item.EncryptedContent != "" {
		return canonical.CanonicalItem{}, false, canonical.UnsupportedOperation("responses manual reasoning state is not supported in P0")
	}
	parts := make([]canonical.ReasoningPart, 0, len(item.Summary))
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
	if len(parts) == 0 {
		return canonical.CanonicalItem{}, false, nil
	}
	reasoning, err := canonical.NewReasoningItem(parts, canonical.OpaqueThinking{})
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
