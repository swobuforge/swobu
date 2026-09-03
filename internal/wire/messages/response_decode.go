package messages

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
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
		{"message", "usage", "input_tokens"},
		{"usage", "prompt_tokens"},
		{"usageMetadata", "promptTokenCount"},
		{"usage", "inputTokens"},
	},
	OutputPaths: [][]string{
		{"usage", "output_tokens"},
		{"message", "usage", "output_tokens"},
		{"usage", "completion_tokens"},
		{"usageMetadata", "candidatesTokenCount"},
		{"usage", "outputTokens"},
	},
	ReasoningPaths: [][]string{
		{"message", "usage", "output_tokens_details", "thinking_tokens"},
		{"usage", "output_tokens_details", "thinking_tokens"},
	},
	CacheReadPaths: [][]string{
		{"message", "usage", "cache_read_input_tokens"},
		{"usage", "cache_read_input_tokens"},
		{"usage", "input_tokens_details", "cached_tokens"},
		{"usage", "prompt_tokens_details", "cached_tokens"},
		{"usageMetadata", "cachedContentTokenCount"},
		{"usage", "cacheReadInputTokens"},
	},
	CacheWritePaths: [][]string{
		{"message", "usage", "cache_creation_input_tokens"},
		{"usage", "cache_creation_input_tokens"},
		{"usage", "input_tokens_details", "cache_write_tokens"},
		{"usage", "prompt_tokens_details", "cache_write_tokens"},
		{"usage", "cacheWriteInputTokens"},
	},
}

func decodeResponseBuffered(ctx context.Context, request canonical.CanonicalRequest, names wire.ToolNames, raw []byte, exchangeID string, changeLog *[]compat.Change) (canonical.ResponseStream, error) {
	var dto bufferedResponseBody
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, canonical.InternalError("messages response is invalid JSON")
	}
	usage := extractMessagesUsage(raw).canonical()
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
	for index, rawBlock := range dto.Content {
		var block bufferedContentBlockBody
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, canonical.InternalError("messages response content block is invalid")
		}
		blockType := strings.TrimSpace(block.Type) // swobu:io-string source=boundary // swobu:io-string source=provider-wire
		if blockType == "" {
			return nil, canonical.NewBackendError("messages", 0, "messages response content block is missing type", "")
		}
		switch blockType {
		case "text":
			part, err := decodeMessagesCitedText(block.Text, block.Citations, messagesProjectionEvidence{
				feature: canonical.ResponseItemsKind, changeLog: changeLog, exchangeID: exchangeID,
				occurrence: canonical.ResponseItemOccurrence(uint32(index)),
			})
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
			key, executor, err := decodeMessagesNamedCallable(names, environment, strings.TrimSpace(block.Name)) // swobu:io-string source=boundary
			if err != nil {
				return nil, canonical.InternalError("messages response tool_use references an unknown or ambiguous tool")
			}
			var item canonical.CanonicalItem
			if key.Kind() == canonical.ToolKindDiscovery {
				item, err = canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(object), executor)
			} else {
				item, err = canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(object))
			}
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
			name := strings.TrimSpace(block.Name) // swobu:io-string source=provider-wire
			if name == toolSearchRegexName || name == toolSearchNaturalLanguageName {
				if err := flushMessage(); err != nil {
					return nil, canonical.InternalError("messages response message item is invalid")
				}
				callID, err := canonical.NewToolCallID(block.ID)
				if err != nil {
					return nil, canonical.InternalError("messages response discovery call is missing id")
				}
				object, err := canonical.ParseJSONObject(block.Input)
				if err != nil {
					return nil, canonical.InternalError("messages response discovery call input is invalid")
				}
				if _, err := messagesProviderDiscoveryForName(request, name); err != nil {
					return nil, err
				}
				item, err := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(object), canonical.DiscoveryExecutorProvider)
				if err != nil {
					return nil, canonical.InternalError("messages response discovery call is invalid")
				}
				items = append(items, item)
				continue
			}
			if name != "web_search" {
				if err := appendMessagesOccurrenceChange(changeLog, exchangeID, canonical.ResponseItemsKind, compat.Omission, canonical.ResponseItemOccurrence(uint32(index))); err != nil {
					return nil, err
				}
				continue
			}
			if err := flushMessage(); err != nil {
				return nil, canonical.InternalError("messages response message item is invalid")
			}
			item, err := decodeMessagesWebSearchCall(block.ID, block.Input, messagesProjectionEvidence{
				feature: canonical.ResponseItemsKind, changeLog: changeLog, exchangeID: exchangeID,
				occurrence: canonical.ResponseItemOccurrence(uint32(index)),
			})
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		case "web_search_tool_result":
			if err := flushMessage(); err != nil {
				return nil, canonical.InternalError("messages response message item is invalid")
			}
			item, err := decodeMessagesWebSearchResult(block.ToolUseID, block.Content, block.IsError, messagesProjectionEvidence{
				feature: canonical.ResponseItemsKind, changeLog: changeLog, exchangeID: exchangeID,
				occurrence: canonical.ResponseItemOccurrence(uint32(index)),
			})
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		case "tool_search_tool_result":
			if err := flushMessage(); err != nil {
				return nil, canonical.InternalError("messages response message item is invalid")
			}
			item, err := decodeMessagesDiscoveryResult(request, names, block.ToolUseID, block.Content)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		default:
			if err := appendMessagesOccurrenceChange(changeLog, exchangeID, canonical.ResponseItemsKind, compat.Omission, canonical.ResponseItemOccurrence(uint32(index))); err != nil {
				return nil, err
			}
			continue
		}
	}
	if err := flushMessage(); err != nil {
		return nil, canonical.InternalError("messages response message item is invalid")
	}
	if err := validateMessagesResponseResidual(items, dto.StopReason, len(dto.Content)); err != nil {
		return nil, err
	}
	return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		exchangeID,
		canonical.ResponseRef{},
		dto.Model,
		items,
		messagesCompletion(dto.StopReason),
		usage,
	)), nil
}

type messagesUsageFamily uint8

const (
	messagesUsageFamilyUnknown messagesUsageFamily = iota
	messagesUsageFamilyNativeCacheExclusive
	messagesUsageFamilyCompatibilityCacheInclusive
)

type messagesUsageSnapshot struct {
	family messagesUsageFamily
	usage  canonical.TokenUsage
}

func extractMessagesUsage(raw []byte) messagesUsageSnapshot {
	usageRaw := messagesUsageObject(raw)
	var usageFields map[string]json.RawMessage
	if json.Unmarshal(usageRaw, &usageFields) == nil {
		if _, native := usageFields["input_tokens"]; native {
			return messagesUsageSnapshot{
				family: messagesUsageFamilyNativeCacheExclusive,
				usage: core.ExtractTokenUsage(usageRaw, core.TokenUsagePathSpec{
					InputPaths:      [][]string{{"input_tokens"}},
					OutputPaths:     [][]string{{"output_tokens"}},
					ReasoningPaths:  [][]string{{"output_tokens_details", "thinking_tokens"}},
					CacheReadPaths:  [][]string{{"cache_read_input_tokens"}},
					CacheWritePaths: [][]string{{"cache_creation_input_tokens"}},
				}),
			}
		}
		if _, compatible := usageFields["prompt_tokens"]; compatible {
			return messagesUsageSnapshot{
				family: messagesUsageFamilyCompatibilityCacheInclusive,
				usage: core.ExtractTokenUsage(usageRaw, core.TokenUsagePathSpec{
					InputPaths:     [][]string{{"prompt_tokens"}},
					OutputPaths:    [][]string{{"completion_tokens"}},
					ReasoningPaths: [][]string{{"output_tokens_details", "thinking_tokens"}},
					CacheReadPaths: [][]string{{"prompt_tokens_details", "cached_tokens"}},
				}),
			}
		}
	}
	return messagesUsageSnapshot{
		usage: core.ExtractTokenUsage(raw, tokenUsagePathSpec),
	}
}

func messagesUsageObject(raw []byte) json.RawMessage {
	var payload map[string]json.RawMessage
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	usageRaw := payload["usage"]
	if len(usageRaw) == 0 {
		var message map[string]json.RawMessage
		if json.Unmarshal(payload["message"], &message) == nil {
			usageRaw = message["usage"]
		}
	}
	return usageRaw
}

func (snapshot messagesUsageSnapshot) canonical() canonical.TokenUsage {
	usage := snapshot.usage
	input, hasInput := usage.InputTokens()
	cacheRead, hasCacheRead := usage.CacheReadTokens()
	cacheWrite, hasCacheWrite := usage.CacheWriteTokens()
	if hasInput && snapshot.family == messagesUsageFamilyNativeCacheExclusive {
		if hasCacheRead {
			input += cacheRead
		}
		if hasCacheWrite {
			input += cacheWrite
		}
	}
	output, hasOutput := usage.OutputTokens()
	reasoning, hasReasoning := usage.ReasoningTokens()
	pointer := func(value int, known bool) *int {
		if !known {
			return nil
		}
		return &value
	}
	normalized, _ := canonical.NewTokenUsage(canonical.TokenUsageParams{
		InputTokens: pointer(input, hasInput), OutputTokens: pointer(output, hasOutput),
		ReasoningTokens: pointer(reasoning, hasReasoning), CacheReadTokens: pointer(cacheRead, hasCacheRead),
		CacheWriteTokens: pointer(cacheWrite, hasCacheWrite),
	})
	return normalized
}

func messagesProviderDiscoveryForName(request canonical.CanonicalRequest, name string) (canonical.ToolDiscoveryTool, error) {
	environment, err := canonical.EffectiveTools(request)
	if err != nil {
		return canonical.ToolDiscoveryTool{}, canonical.InternalError("messages discovery tool environment is ambiguous")
	}
	declaration, ok := environment.Lookup(canonical.ToolDiscoveryKey())
	if !ok {
		return canonical.ToolDiscoveryTool{}, canonical.NewBackendError("messages", 0, "messages discovery call has no declared search tool", "")
	}
	discovery, ok := declaration.Discovery()
	if !ok || discovery.Executor() != canonical.DiscoveryExecutorProvider {
		return canonical.ToolDiscoveryTool{}, canonical.NewBackendError("messages", 0, "messages discovery call does not match provider execution", "")
	}
	want, err := messagesProviderDiscoveryName(discovery)
	if err != nil || want != name {
		return canonical.ToolDiscoveryTool{}, canonical.NewBackendError("messages", 0, "messages discovery call does not match declared query semantics", "")
	}
	return discovery, nil
}

func decodeMessagesNamedCallable(names wire.ToolNames, environment canonical.ToolEnvironment, name string) (canonical.ToolKey, canonical.DiscoveryExecutor, error) {
	if key, err := wire.DecodeToolKey(names, environment, canonical.ToolKindFunction, name); err == nil {
		return key, 0, nil
	}
	key, err := wire.DecodeToolKey(names, environment, canonical.ToolKindDiscovery, name)
	if err != nil {
		return canonical.ToolKey{}, 0, err
	}
	declaration, _ := environment.Lookup(key)
	discovery, _ := declaration.Discovery()
	return key, discovery.Executor(), nil
}

func decodeMessagesDiscoveryResult(request canonical.CanonicalRequest, names wire.ToolNames, rawCallID string, raw json.RawMessage) (canonical.CanonicalItem, error) {
	callID, err := canonical.NewToolCallID(rawCallID)
	if err != nil {
		return canonical.CanonicalItem{}, canonical.InternalError("messages discovery result is missing tool_use_id")
	}
	var content struct {
		Type           string `json:"type"`
		ErrorCode      string `json:"error_code"`
		ErrorMessage   string `json:"error_message"`
		ToolReferences []struct {
			Type     string `json:"type"`
			ToolName string `json:"tool_name"`
		} `json:"tool_references"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		return canonical.CanonicalItem{}, canonical.NewBackendError("messages", 0, "messages discovery result is invalid", "")
	}
	if content.Type == "tool_search_tool_result_error" {
		code := canonical.Unspecified[string]()
		if strings.TrimSpace(content.ErrorCode) != "" {
			code = canonical.Specify(strings.TrimSpace(content.ErrorCode))
		}
		item, err := canonical.NewToolDiscoveryFailureItem(callID, canonical.DiscoveryExecutorProvider, code, content.ErrorMessage)
		if err != nil {
			return canonical.CanonicalItem{}, canonical.InternalError("messages discovery failure is invalid")
		}
		return item, nil
	}
	if content.Type != "tool_search_tool_search_result" {
		return canonical.CanonicalItem{}, canonical.NewBackendError("messages", 0, "messages discovery result is invalid", "")
	}
	environment, err := canonical.EffectiveTools(request)
	if err != nil {
		return canonical.CanonicalItem{}, canonical.InternalError("messages discovery result tool environment is ambiguous")
	}
	declarations := make([]canonical.ToolDeclaration, 0, len(content.ToolReferences))
	for _, reference := range content.ToolReferences {
		declaration, err := resolveMessagesProviderToolReference(names, environment, reference.ToolName)
		if err != nil {
			return canonical.CanonicalItem{}, err
		}
		declarations = append(declarations, declaration)
	}
	tools, err := canonical.NewToolSet(declarations)
	if err != nil {
		return canonical.CanonicalItem{}, canonical.InternalError("messages discovery result tools are invalid")
	}
	item, err := canonical.NewToolDiscoveryResultItem(callID, tools, canonical.DiscoveryExecutorProvider)
	if err != nil {
		return canonical.CanonicalItem{}, canonical.InternalError("messages discovery result is invalid")
	}
	return item, nil
}

func validateMessagesResponseResidual(items []canonical.CanonicalItem, stopReason string, wireItems int) error {
	if wireItems > 0 && len(items) == 0 {
		return canonical.NewBackendError("", 0, "backend produced no usable canonical output", "")
	}
	if strings.TrimSpace(stopReason) != "tool_use" {
		return nil
	}
	for _, item := range items {
		if _, ok := item.ToolCall(); ok {
			return nil
		}
	}
	return canonical.NewBackendError("", 0, "messages stop reason requires a surviving tool call", "")
}

func messagesResponseReasoningKind(request canonical.CanonicalRequest) canonical.ReasoningPartKind {
	if disclosure, ok := request.Reasoning().DisclosureField().Get(); ok && disclosure == canonical.ReasoningDisclosureSummary {
		return canonical.ReasoningPartSummary
	}
	return canonical.ReasoningPartTrace
}
