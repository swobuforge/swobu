// event-state machine together so migration behavior stays recoverable.
package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
	openaicompat "github.com/swobuforge/swobu/internal/wire/openai"
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

func decodeResponseBuffered(ctx context.Context, raw []byte, exchangeID string, sink effect.Sink) (canonical.EventReader, error) {
	var dto responseEnvelope
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, canonical.InternalError("responses output is invalid JSON")
	}
	usage := core.ExtractTokenUsage(raw, tokenUsagePathSpec)
	_, inputPresent := usage.InputTokens()
	openaicompat.EmitUsageCompatibilityEffect(ctx, sink, exchangeID, inputPresent, compat.UsageInputTokens, compat.Subject("wire:/usage/input_tokens"))
	_, outputPresent := usage.OutputTokens()
	openaicompat.EmitUsageCompatibilityEffect(ctx, sink, exchangeID, outputPresent, compat.UsageOutputTokens, compat.Subject("wire:/usage/output_tokens"))
	_, reasoningPresent := usage.ReasoningTokens()
	openaicompat.EmitUsageCompatibilityEffect(ctx, sink, exchangeID, reasoningPresent, compat.UsageReasoningTokens, compat.Subject("wire:/usage/output_tokens_details/reasoning_tokens"))
	_, cacheReadPresent := usage.CacheReadTokens()
	openaicompat.EmitUsageCompatibilityEffect(ctx, sink, exchangeID, cacheReadPresent, compat.UsageCacheReadTokens, compat.Subject("wire:/usage/cache_read_tokens"))
	_, cacheWritePresent := usage.CacheWriteTokens()
	openaicompat.EmitUsageCompatibilityEffect(ctx, sink, exchangeID, cacheWritePresent, compat.UsageCacheWriteTokens, compat.Subject("wire:/usage/cache_write_tokens"))
	if terminalReason, promptBlocked := responsesTerminalReason("", dto.Status, "", dto.ContentFilters, responseIncompleteReason(dto.IncompleteDetails)); promptBlocked {
		message := responsesContentFilterMessage(responsesBlockedContentFilterSource(dto.ContentFilters))
		return nil, canonical.NewBackendError("responses", http.StatusForbidden, message, "")
	} else {
		items, err := decodeOutputItems(ctx, dto.Output, dto.OutputText, exchangeID, sink)
		if err != nil {
			return nil, err
		}
		return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
			exchangeID,
			dto.ID,
			dto.Model,
			items,
			terminalReason,
			usage,
		)), nil
	}
}

func decodeOutputItems(ctx context.Context, items []responsesWireOutputItemDTO, outputText string, exchangeID string, sink effect.Sink) ([]canonical.OutputItem, error) {
	output := make([]canonical.OutputItem, 0, len(items))
	for idx, item := range items {
		itemType := strings.TrimSpace(item.Type) // swobu:io-string source=provider-wire
		switch itemType {
		case "message":
			parts, err := openaicompat.DecodeContentParts(item.Content, "responses message content is invalid")
			if err != nil {
				return nil, canonical.InternalError("responses message content is invalid")
			}
			err = openaicompat.WalkContentParts(parts, func(idx int, part openaicompat.ContentPartItem) error {
				partType := strings.TrimSpace(part.Type) // swobu:io-string source=boundary
				switch partType {
				case "text", "output_text", "input_text":
					output = append(output, canonical.NewTextOutputItem(fmt.Sprintf("text_%d", len(output)+idx), part.Text))
				default:
					return canonical.UnsupportedOperation("responses output item content part type is not implemented")
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		case "function_call", "mcp_call":
			rawArgs := item.Arguments
			if rawArgs != "" {
				decoded := map[string]any{}
				if err := json.Unmarshal([]byte(rawArgs), &decoded); err != nil {
					return nil, canonical.InternalError("responses tool call arguments are invalid")
				}
			}
			itemID := strings.TrimSpace(item.ID) // swobu:io-string source=boundary
			if itemID == "" {
				itemID = fallbackItemID("", item.CallID, nil)
			}
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			name := strings.TrimSpace(item.Name)     // swobu:io-string source=boundary
			if callID == "" {
				callID = itemID
			}
			output = append(output, canonical.NewToolUseOutputItem(
				itemID,
				callID,
				name,
				canonical.NewToolArgumentsObject(rawArgs),
			))
		case "custom_tool_call":
			itemID := strings.TrimSpace(item.ID) // swobu:io-string source=boundary
			if itemID == "" {
				itemID = fallbackItemID("", item.CallID, nil)
			}
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			name := strings.TrimSpace(item.Name)     // swobu:io-string source=boundary
			if callID == "" {
				callID = itemID
			}
			output = append(output, canonical.NewCustomToolUseOutputItem(
				itemID,
				callID,
				name,
				canonical.NewToolArgumentsObject(item.Input),
			))
		case "reasoning":
			if sink != nil {
				_ = sink.Commit(ctx, exchangeID, []effect.Effect{
					effect.CompatibilityEffect{
						Feature: compat.ResponseReasoning,
						Outcome: compat.Reject,
						Subject: compat.Subject(fmt.Sprintf("wire:/output/%d/type", idx)),
					},
				})
			}
			return nil, canonical.UnsupportedOperation("responses reasoning output is not supported by swobu v0")
		default:
			return nil, canonical.UnsupportedOperation("responses output item type is not implemented")
		}
	}
	if len(output) == 0 && strings.TrimSpace(outputText) != "" { // swobu:io-string source=boundary
		output = append(output, canonical.NewTextOutputItem("text_0", outputText))
	}
	return output, nil
}
