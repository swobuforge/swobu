package messages

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

type bufferedResponseBody struct {
	ID         string                     `json:"id"`
	Model      string                     `json:"model"`
	Content    []bufferedContentBlockBody `json:"content"`
	StopReason string                     `json:"stop_reason"`
}

type bufferedContentBlockBody struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
}

var tokenUsagePathSpec = core.TokenUsagePathSpec{
	InputPaths: [][]string{
		{"usage", "input_tokens"},
		{"usage", "prompt_tokens"},
		{"usageMetadata", "promptTokenCount"},
		{"usage", "inputTokens"},
	},
	OutputPaths: [][]string{
		{"usage", "output_tokens"},
		{"usage", "completion_tokens"},
		{"usageMetadata", "candidatesTokenCount"},
		{"usage", "outputTokens"},
	},
	CacheReadPaths: [][]string{
		{"usage", "cache_read_input_tokens"},
		{"usage", "input_tokens_details", "cached_tokens"},
		{"usage", "prompt_tokens_details", "cached_tokens"},
		{"usageMetadata", "cachedContentTokenCount"},
		{"usage", "cacheReadInputTokens"},
	},
	CacheWritePaths: [][]string{
		{"usage", "cache_creation_input_tokens"},
		{"usage", "input_tokens_details", "cache_write_tokens"},
		{"usage", "prompt_tokens_details", "cache_write_tokens"},
		{"usage", "cacheWriteInputTokens"},
	},
}

func decodeResponseBuffered(ctx context.Context, request canonical.CanonicalRequest, raw []byte, exchangeID string, sink compat.Sink) (canonical.ResponseStream, error) {
	var dto bufferedResponseBody
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, canonical.InternalError("messages response is invalid JSON")
	}
	usage := core.ExtractTokenUsage(raw, tokenUsagePathSpec)
	items := make([]canonical.CanonicalItem, 0, len(dto.Content))
	textParts := make([]canonical.MessagePart, 0)
	flushMessage := func() error {
		if len(textParts) == 0 {
			return nil
		}
		message, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, textParts)
		if err != nil {
			return err
		}
		items = append(items, message)
		textParts = nil
		return nil
	}
	for _, block := range dto.Content {
		blockType := strings.TrimSpace(block.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
		switch blockType {
		case "text":
			textParts = append(textParts, canonical.NewTextMessagePart(block.Text))
		case "tool_use":
			if err := flushMessage(); err != nil {
				return nil, canonical.InternalError("messages response message item is invalid")
			}
			callID, err := canonical.NewToolCallID(block.ID)
			if err != nil {
				return nil, canonical.InternalError("messages response tool_use is missing id")
			}
			object, err := canonical.ParseJSONObject(block.Input)
			if err != nil {
				return nil, canonical.InternalError("messages response tool_use input is invalid JSON object")
			}
			resolved, _, err := canonical.ResolveToolDeclarationByName(request.Tools(), strings.TrimSpace(block.Name), canonical.ToolTypeFunction) // swobu:io-string source=boundary
			if err != nil {
				return nil, canonical.InternalError("messages response tool_use references an unknown or ambiguous tool")
			}
			item, err := canonical.NewToolCallItem(callID, resolved.Key(), canonical.NewJSONObjectToolInput(object))
			if err != nil {
				return nil, canonical.InternalError("messages response tool_use is invalid")
			}
			items = append(items, item)
		case "server_tool_use", "web_search_tool_result":
			return nil, canonical.UnsupportedOperation("messages provider tool lifecycle output is not implemented")
		default:
			return nil, canonical.InternalError("messages response content block is unsupported")
		}
	}
	if err := flushMessage(); err != nil {
		return nil, canonical.InternalError("messages response message item is invalid")
	}
	_, inputPresent := usage.InputTokens()
	openaiwire.EmitUsageDecision(ctx, sink, exchangeID, inputPresent, compat.ResponseUsageInputTokens, compat.Subject("wire:/usage/input_tokens"))
	_, outputPresent := usage.OutputTokens()
	openaiwire.EmitUsageDecision(ctx, sink, exchangeID, outputPresent, compat.ResponseUsageOutputTokens, compat.Subject("wire:/usage/output_tokens"))
	_, cacheReadPresent := usage.CacheReadTokens()
	openaiwire.EmitUsageDecision(ctx, sink, exchangeID, cacheReadPresent, compat.ResponseUsageCacheReadTokens, compat.Subject("wire:/usage/cache_read_tokens"))
	_, cacheWritePresent := usage.CacheWriteTokens()
	openaiwire.EmitUsageDecision(ctx, sink, exchangeID, cacheWritePresent, compat.ResponseUsageCacheWriteTokens, compat.Subject("wire:/usage/cache_write_tokens"))
	return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		exchangeID,
		canonical.ResponseRef{},
		dto.Model,
		items,
		dto.StopReason,
		usage,
	)), nil
}
