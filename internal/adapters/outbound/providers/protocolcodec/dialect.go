package protocolcodec

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/cachelocality"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
	"github.com/swobuforge/swobu/internal/wire/messages"
	"github.com/swobuforge/swobu/internal/wire/responses"
)

// AttemptDecoration carries attempt-specific top-level fields (Phase D)
// and carrier metadata to attach to the outbound request document.
type AttemptDecoration struct {
	Fields map[string]any
	Meta   carrier.Meta
}

// AttemptContext contains attempt-scoped transport/routing context and cache placement.
// It deliberately excludes canonical request semantics to preserve Phase D boundary integrity.
type AttemptContext struct {
	CacheLocality         cachelocality.Key
	HasNextRouteCandidate bool
}

// AttemptDecorator adds attempt-specific non-semantic fields or carrier metadata
// after semantic lowering and before the single serialization boundary.
type AttemptDecorator func(ctx AttemptContext) (AttemptDecoration, error)

// ChatDialect contains executable target rules for semantic occurrences that
// differ from standard Chat Completions. Zero values use standard lowering.
type ChatDialect struct {
	LowerTool              chatcompletions.ToolLoweringRule
	LowerToolPolicy        chatcompletions.ToolPolicyLoweringRule
	LowerReasoning         chatcompletions.ReasoningLoweringRule
	LowerMessage           chatcompletions.MessageLoweringRule
	UseMaxCompletionTokens bool
	ResponseReasoning      func() ChatReasoningExtractor
	DecorateAttempt        AttemptDecorator
}

// ResponsesDialect contains executable target rules for semantic occurrences
// that differ from official Responses. Zero values use standard lowering.
type ResponsesDialect struct {
	LowerTool                    responses.ToolLoweringRule
	LowerToolPolicy              responses.ToolPolicyLoweringRule
	PrependInstructionsToInput   bool
	OmitInclude                  bool
	OmitStoreFalse               bool
	ForceArrayInput              bool
	DefaultStore                 *bool
	RequireStreamingSSE          bool
	CaptureResponsesContinuation bool
	DecorateAttempt              AttemptDecorator
}

// MessagesDialect contains executable target rules for semantic occurrences
// that differ from standard Messages. Zero values use standard lowering.
type MessagesDialect struct {
	LowerTool            messages.ToolLoweringRule
	LowerToolPolicy      messages.ToolPolicyLoweringRule
	OmitAdaptiveThinking bool
	DecorateAttempt      AttemptDecorator
}

// ChatHostedSearchTool lowers canonical hosted search for Chat Completions.
func ChatHostedSearchTool(fragment func() any, spelling string) chatcompletions.ToolLoweringRule {
	return func(_ chatcompletions.ToolLoweringContext, tool canonical.ToolDeclaration) ([]any, bool, []compat.Change, error) {
		if tool.Kind() != canonical.ToolKindWebSearch {
			return nil, false, nil, nil
		}
		if fragment != nil {
			return []any{fragment()}, true, nil, nil
		}
		if spelling == "" {
			return nil, true, []compat.Change{{Capability: canonical.RequestToolsKind, Occurrence: canonical.ToolOccurrence(tool.Key()), Kind: compat.Omission}}, nil
		}
		return []any{chatcompletions.ProviderRequestTool{Type: spelling}}, true, nil, nil
	}
}

// ChatHostedSearchToolPolicy resolves a specific hosted-search policy for Chat Completions.
func ChatHostedSearchToolPolicy(spelling string) chatcompletions.ToolPolicyLoweringRule {
	return func(policy canonical.ToolPolicy, lowered wire.LoweredToolSet, _ wire.ToolNames) (any, bool, []compat.Change, error) {
		if policy.Mode != canonical.ToolPolicySpecific {
			return nil, false, nil, nil
		}
		key, ok := policy.SpecificID()
		if !ok || key.Kind() != canonical.ToolKindWebSearch {
			return nil, false, nil, nil
		}
		record, ok := lowered.FindSource(key)
		if !ok {
			return nil, true, nil, canonical.BadRequest(fmt.Sprintf("tool %q is not present in the tool declaration set", key))
		}
		if record.FragmentCount == 0 {
			return nil, true, nil, provider.NewIncompatibleTarget(fmt.Sprintf("target lowering produced 0 fragments for tool %q", key))
		}
		if record.FragmentCount != 1 {
			return nil, true, nil, provider.NewIncompatibleTarget(fmt.Sprintf("1->N lowered tool %q requires explicit provider tool policy lowering rule for specific selection", key))
		}
		if spelling == "" {
			return nil, true, nil, provider.NewIncompatibleTarget("target cannot require hosted search specifically")
		}
		return map[string]any{"type": spelling}, true, nil, nil
	}
}

// ChatOutOfBandHostedSearch consumes the canonical hosted-search declaration
// when a provider enables search through top-level request parameters rather
// than a Chat tool fragment.
func ChatOutOfBandHostedSearch() chatcompletions.ToolLoweringRule {
	return func(_ chatcompletions.ToolLoweringContext, tool canonical.ToolDeclaration) ([]any, bool, []compat.Change, error) {
		if tool.Kind() != canonical.ToolKindWebSearch {
			return nil, false, nil, nil
		}
		return nil, true, nil, nil
	}
}

// ChatOutOfBandHostedSearchPolicy consumes specific hosted-search selection
// after proving the corresponding canonical declaration was lowered.
func ChatOutOfBandHostedSearchPolicy() chatcompletions.ToolPolicyLoweringRule {
	return func(policy canonical.ToolPolicy, lowered wire.LoweredToolSet, _ wire.ToolNames) (any, bool, []compat.Change, error) {
		if policy.Mode == canonical.ToolPolicyRequired {
			if lowered.Len() == 0 {
				return nil, false, nil, nil
			}
			for _, record := range lowered.Records {
				if record.Kind != canonical.ToolKindWebSearch || record.FragmentCount != 0 {
					return nil, false, nil, nil
				}
			}
			return nil, true, nil, nil
		}
		if policy.Mode != canonical.ToolPolicySpecific {
			return nil, false, nil, nil
		}
		key, ok := policy.SpecificID()
		if !ok || key.Kind() != canonical.ToolKindWebSearch {
			return nil, false, nil, nil
		}
		if _, ok := lowered.FindSource(key); !ok {
			return nil, true, nil, canonical.BadRequest("specific web-search tool is not declared")
		}
		return nil, true, nil, nil
	}
}

// ResponsesHostedSearchTool lowers canonical hosted search for Responses.
func ResponsesHostedSearchTool(spelling string) responses.ToolLoweringRule {
	return func(_ responses.ToolLoweringContext, tool canonical.ToolDeclaration) ([]responses.ProviderRequestTool, bool, []compat.Change, error) {
		if tool.Kind() != canonical.ToolKindWebSearch {
			return nil, false, nil, nil
		}
		if spelling == "" {
			return nil, true, []compat.Change{{Capability: canonical.RequestToolsKind, Occurrence: canonical.ToolOccurrence(tool.Key()), Kind: compat.Omission}}, nil
		}
		return []responses.ProviderRequestTool{{Type: spelling}}, true, nil, nil
	}
}

// ResponsesHostedSearchToolPolicy resolves a specific hosted-search policy for Responses.
func ResponsesHostedSearchToolPolicy(spelling string) responses.ToolPolicyLoweringRule {
	return func(policy canonical.ToolPolicy, lowered wire.LoweredToolSet, _ wire.ToolNames) (any, bool, []compat.Change, error) {
		if policy.Mode != canonical.ToolPolicySpecific {
			return nil, false, nil, nil
		}
		key, ok := policy.SpecificID()
		if !ok || key.Kind() != canonical.ToolKindWebSearch {
			return nil, false, nil, nil
		}
		record, ok := lowered.FindSource(key)
		if !ok {
			return nil, true, nil, canonical.BadRequest(fmt.Sprintf("tool %q is not present in the tool declaration set", key))
		}
		if record.FragmentCount == 0 {
			return nil, true, nil, provider.NewIncompatibleTarget(fmt.Sprintf("target lowering produced 0 fragments for tool %q", key))
		}
		if record.FragmentCount != 1 {
			return nil, true, nil, provider.NewIncompatibleTarget(fmt.Sprintf("1->N lowered tool %q requires explicit provider tool policy lowering rule for specific selection", key))
		}
		if spelling == "" {
			return nil, true, nil, provider.NewIncompatibleTarget("target cannot require hosted search specifically")
		}
		return map[string]any{"type": spelling}, true, nil, nil
	}
}

// MessagesHostedSearchTool lowers canonical hosted search for Messages (e.g. Anthropic).
func MessagesHostedSearchTool(typeName string, allowedCallers ...string) messages.ToolLoweringRule {
	return func(_ messages.ToolLoweringContext, tool canonical.ToolDeclaration) ([]messages.ProviderRequestTool, bool, []compat.Change, error) {
		if tool.Kind() != canonical.ToolKindWebSearch {
			return nil, false, nil, nil
		}
		if typeName == "" {
			return nil, true, []compat.Change{{Capability: canonical.RequestToolsKind, Occurrence: canonical.ToolOccurrence(tool.Key()), Kind: compat.Omission}}, nil
		}
		toolDTO := messages.ProviderRequestTool{
			Type: typeName,
			Name: canonical.WebSearchToolKey().Name(),
		}
		if len(allowedCallers) > 0 {
			toolDTO.AllowedCallers = allowedCallers
		}
		return []messages.ProviderRequestTool{toolDTO}, true, nil, nil
	}
}

// MessagesHostedSearchToolPolicy resolves a specific hosted-search policy for Messages.
func MessagesHostedSearchToolPolicy(typeName string) messages.ToolPolicyLoweringRule {
	return func(policy canonical.ToolPolicy, lowered wire.LoweredToolSet, _ wire.ToolNames) (any, bool, []compat.Change, error) {
		if policy.Mode != canonical.ToolPolicySpecific {
			return nil, false, nil, nil
		}
		key, ok := policy.SpecificID()
		if !ok || key.Kind() != canonical.ToolKindWebSearch {
			return nil, false, nil, nil
		}
		record, ok := lowered.FindSource(key)
		if !ok || record.FragmentCount == 0 {
			return nil, true, nil, canonical.BadRequest(fmt.Sprintf("tool %q is not present in the tool declaration set", key))
		}
		if typeName == "" {
			return nil, true, nil, provider.NewIncompatibleTarget("target cannot require hosted search specifically")
		}
		return map[string]any{"type": "tool", "name": canonical.WebSearchToolKey().Name()}, true, nil, nil
	}
}

// Re-exports of message-level chatcompletions symbols for provider packages.
type ChatMessageLoweringRule = chatcompletions.MessageLoweringRule
type ChatProviderRequestMessage = chatcompletions.ProviderRequestMessage
