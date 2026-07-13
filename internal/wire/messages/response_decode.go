package messages

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
	openaicompat "github.com/swobuforge/swobu/internal/wire/openai"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

type bufferedResponseBody struct {
	ID         string                     `json:"id"`
	Model      string                     `json:"model"`
	Content    []bufferedContentBlockBody `json:"content"`
	StopReason string                     `json:"stop_reason"`
}

type bufferedContentBlockBody struct {
	Type      string         `json:"type"`
	Text      string         `json:"text"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
	ToolUseID string         `json:"tool_use_id"`
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

func decodeResponseBuffered(ctx context.Context, raw []byte, exchangeID string, sink effect.Sink) (canonical.EventReader, error) {
	var dto bufferedResponseBody
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, canonical.InternalError("messages response is invalid JSON")
	}
	usage := core.ExtractTokenUsage(raw, tokenUsagePathSpec)
	items := make([]canonical.CanonicalItem, 0, len(dto.Content))
	for i, block := range dto.Content {
		blockType := strings.TrimSpace(block.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
		switch blockType {
		case "text":
			items = append(items, canonical.NewTextOutputItem("text_"+strconv.Itoa(i), block.Text))
		case "tool_use":
			itemID := strings.TrimSpace(block.ID) // swobu:io-string source=boundary
			if itemID == "" {
				itemID = "tool_" + strconv.Itoa(i)
			}
			args, marshalErr := json.Marshal(block.Input)
			if marshalErr != nil {
				return nil, canonical.InternalError("messages response tool_use input is invalid JSON object")
			}
			items = append(items, canonical.NewToolUseOutputItem(itemID, strings.TrimSpace(block.ID), strings.TrimSpace(block.Name), canonical.NewToolArgumentsObject(string(args)))) // swobu:io-string source=boundary
		default:
			return nil, canonical.InternalError("messages response content block is unsupported")
		}
	}
	_, inputPresent := usage.InputTokens()
	openaicompat.EmitUsageCompatibilityEffect(ctx, sink, exchangeID, inputPresent, compat.UsageInputTokens, compat.Subject("wire:/usage/input_tokens"))
	_, outputPresent := usage.OutputTokens()
	openaicompat.EmitUsageCompatibilityEffect(ctx, sink, exchangeID, outputPresent, compat.UsageOutputTokens, compat.Subject("wire:/usage/output_tokens"))
	_, cacheReadPresent := usage.CacheReadTokens()
	openaicompat.EmitUsageCompatibilityEffect(ctx, sink, exchangeID, cacheReadPresent, compat.UsageCacheReadTokens, compat.Subject("wire:/usage/cache_read_tokens"))
	_, cacheWritePresent := usage.CacheWriteTokens()
	openaicompat.EmitUsageCompatibilityEffect(ctx, sink, exchangeID, cacheWritePresent, compat.UsageCacheWriteTokens, compat.Subject("wire:/usage/cache_write_tokens"))
	return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		exchangeID,
		dto.ID,
		dto.Model,
		items,
		dto.StopReason,
		usage,
	)), nil
}
