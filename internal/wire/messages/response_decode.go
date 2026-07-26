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
	ID         string            `json:"id"`
	Model      string            `json:"model"`
	Content    []json.RawMessage `json:"content"`
	StopReason string            `json:"stop_reason"`
}

type bufferedContentBlockBody struct {
	Type      string                `json:"type"`
	Text      string                `json:"text"`
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	Input     json.RawMessage       `json:"input"`
	ToolUseID string                `json:"tool_use_id"`
	Thinking  string                `json:"thinking"`
	Signature string                `json:"signature"`
	Data      string                `json:"data"`
	Content   json.RawMessage       `json:"content"`
	IsError   bool                  `json:"is_error"`
	Citations []messagesCitationDTO `json:"citations"`
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
	for _, rawBlock := range dto.Content {
		var block bufferedContentBlockBody
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, canonical.InternalError("messages response content block is invalid")
		}
		blockType := strings.TrimSpace(block.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
		switch blockType {
		case "text":
			part, err := decodeMessagesCitedText(block.Text, block.Citations)
			if err != nil {
				return nil, err
			}
			textParts = append(textParts, part)
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
			environment, err := canonical.EffectiveTools(request)
			if err != nil {
				return nil, canonical.InternalError("messages tool environment is ambiguous")
			}
			resolved, _, err := canonical.ResolveToolDeclarationByName(environment.Declarations(), strings.TrimSpace(block.Name), canonical.ToolTypeFunction) // swobu:io-string source=boundary
			if err != nil {
				return nil, canonical.InternalError("messages response tool_use references an unknown or ambiguous tool")
			}
			item, err := canonical.NewToolCallItem(callID, resolved.Key(), canonical.NewJSONObjectToolInput(object))
			if err != nil {
				return nil, canonical.InternalError("messages response tool_use is invalid")
			}
			items = append(items, item)
		case "thinking":
			if err := flushMessage(); err != nil {
				return nil, canonical.InternalError("messages response message item is invalid")
			}
			opaque, err := canonical.NewMessagesOpaqueThinking(rawBlock)
			if err != nil {
				return nil, canonical.InternalError("messages response thinking signature is invalid")
			}
			var parts []canonical.ReasoningPart
			if block.Thinking != "" {
				part, err := canonical.NewReasoningPart(messagesResponseReasoningKind(request), block.Thinking)
				if err != nil {
					return nil, canonical.InternalError("messages response thinking text is invalid")
				}
				parts = []canonical.ReasoningPart{part}
			}
			item, err := canonical.NewReasoningItem(parts, opaque)
			if err != nil {
				return nil, canonical.InternalError("messages response thinking block is invalid")
			}
			items = append(items, item)
		case "redacted_thinking":
			if err := flushMessage(); err != nil {
				return nil, canonical.InternalError("messages response message item is invalid")
			}
			opaque, err := canonical.NewMessagesOpaqueThinking(rawBlock)
			if err != nil {
				return nil, canonical.InternalError("messages response redacted thinking data is invalid")
			}
			item, err := canonical.NewReasoningItem(nil, opaque)
			if err != nil {
				return nil, canonical.InternalError("messages response redacted thinking block is invalid")
			}
			items = append(items, item)
		case "server_tool_use":
			if err := flushMessage(); err != nil {
				return nil, canonical.InternalError("messages response message item is invalid")
			}
			if strings.TrimSpace(block.Name) != "web_search" { // swobu:io-string source=provider-wire
				return nil, canonical.NotImplemented("Swobu has no canonical projection for this Messages server-tool output type")
			}
			item, err := decodeMessagesWebSearchCall(block.ID, block.Input)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		case "web_search_tool_result":
			if err := flushMessage(); err != nil {
				return nil, canonical.InternalError("messages response message item is invalid")
			}
			item, err := decodeMessagesWebSearchResult(block.ToolUseID, block.Content, block.IsError)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
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

func messagesResponseReasoningKind(request canonical.CanonicalRequest) canonical.ReasoningPartKind {
	if disclosure, ok := request.Reasoning().DisclosureField().Get(); ok && disclosure == canonical.ReasoningDisclosureSummary {
		return canonical.ReasoningPartSummary
	}
	return canonical.ReasoningPartTrace
}
