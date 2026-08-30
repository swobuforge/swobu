package protocolcodec

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/cachelocality"
	"github.com/swobuforge/swobu/internal/domain/canonical"
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

// ReasoningTargetDialect is the wire compiler's narrow empirical reasoning
// context, re-exported for exact-provider lowering rules.
type ReasoningTargetDialect = chatcompletions.ReasoningTargetDialect

// ChatToolLowering, ResponsesToolLowering, and MessagesToolLowering expose the
// typed sparse slot sets at the provider-construction boundary. Provider
// packages select slots here without importing protocol serializer packages.
type ChatToolLowering = chatcompletions.ToolLowering
type ChatLowering = chatcompletions.Lowering
type ResponsesToolLowering = responses.ToolLowering
type ResponsesHistoryMessageRoleTransformer = responses.HistoryMessageRoleTransformer
type MessagesToolLowering = messages.ToolLowering
type MessagesLowering = messages.Lowering
type MessagesReasoningTransformer = messages.ReasoningTransformer

// MessagesOmitAdaptiveReasoning is the sparse semantic override for targets
// whose Messages grammar cannot accept adaptive or budget reasoning controls.
var MessagesOmitAdaptiveReasoning MessagesReasoningTransformer = messages.OmitAdaptiveReasoning

// ChatDialect contains executable target rules for semantic occurrences that
// differ from standard Chat Completions. Zero values use standard lowering.
type ChatDialect struct {
	Lowering               chatcompletions.Lowering
	UseMaxCompletionTokens bool
	ResponseReasoning      func() ChatReasoningExtractor
	DecorateAttempt        AttemptDecorator
}

// ResponsesDialect contains executable target rules for semantic occurrences
// that differ from official Responses. Zero values use standard lowering.
type ResponsesDialect struct {
	Tools                        responses.ToolLowering
	HistoryMessageRole           responses.HistoryMessageRoleTransformer
	PrependInstructionsToInput   bool
	OmitInclude                  bool
	OmitMaxOutputTokens          bool
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
	Lowering        messages.Lowering
	DecorateAttempt AttemptDecorator
}

// ChatHostedSearchTool lowers canonical hosted search for Chat Completions.
func ChatHostedSearchTool(fragment func() any, spelling string) chatcompletions.ToolTransformer {
	return func(_ chatcompletions.ToolLoweringContext, tool canonical.ToolDeclaration) (chatcompletions.ToolProjection, []compat.Change, error) {
		if fragment != nil {
			return chatcompletions.ToolProjection{Fragments: []chatcompletions.ToolFragment{{Value: fragment()}}, TargetType: spelling}, nil, nil
		}
		if spelling == "" {
			return chatcompletions.ToolProjection{}, []compat.Change{{Capability: canonical.RequestToolsKind, Occurrence: canonical.ToolOccurrence(tool.Key()), Kind: compat.Omission}}, nil
		}
		standard := chatcompletions.ProviderRequestTool{Type: spelling}
		return chatcompletions.ToolProjection{Fragments: []chatcompletions.ToolFragment{{Value: standard, Standard: &standard}}, TargetType: spelling}, nil, nil
	}
}

// ChatOutOfBandHostedSearch consumes the canonical hosted-search declaration
// when a provider enables search through top-level request parameters rather
// than a Chat tool fragment.
func ChatOutOfBandHostedSearch() chatcompletions.ToolTransformer {
	return func(_ chatcompletions.ToolLoweringContext, _ canonical.ToolDeclaration) (chatcompletions.ToolProjection, []compat.Change, error) {
		return chatcompletions.ToolProjection{TargetType: "out_of_band"}, nil, nil
	}
}

// ResponsesHostedSearchTool lowers canonical hosted search for Responses.
// Source disclosure must be established independently for the exact target.
func ResponsesHostedSearchTool(spelling string, supportsSourceInclude bool) responses.ToolTransformer {
	return func(_ responses.ToolLoweringContext, tool canonical.ToolDeclaration) (responses.ToolProjection, []compat.Change, error) {
		if spelling == "" {
			return responses.ToolProjection{}, []compat.Change{{Capability: canonical.RequestToolsKind, Occurrence: canonical.ToolOccurrence(tool.Key()), Kind: compat.Omission}}, nil
		}
		return responses.HostedSearchProjection(responses.ProviderRequestTool{Type: spelling}, supportsSourceInclude), nil, nil
	}
}

// ResponsesCustomAsFunction lowers one canonical Custom declaration through
// the ordinary Responses function carrier. Exact targets may opt into this
// rule only after their native and wrapper history carriers are characterized.
func ResponsesCustomAsFunction() responses.ToolTransformer {
	return func(ctx responses.ToolLoweringContext, tool canonical.ToolDeclaration) (responses.ToolProjection, []compat.Change, error) {
		custom, ok := tool.Custom()
		if !ok {
			return responses.ToolProjection{}, nil, canonical.InternalError("Responses Custom slot received a non-Custom declaration")
		}
		name, err := wire.EncodeToolName(ctx.Names, tool.Key())
		if err != nil {
			return responses.ToolProjection{}, nil, err
		}
		parameters := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{"type": "string"},
			},
			"required":             []string{"input"},
			"additionalProperties": false,
		}
		var changes []compat.Change
		if !custom.Format().IsEmpty() {
			changes = []compat.Change{compat.NewApproximation(canonical.RequestToolsKind, canonical.ToolOccurrence(tool.Key()))}
		}
		encoded := responses.ProviderRequestTool{
			Type: "function", Name: name, Description: custom.Description(), Parameters: parameters,
		}
		return responses.CustomAsFunctionProjection(encoded, "input"), changes, nil
	}
}

// MessagesHostedSearchTool lowers canonical hosted search for Messages (e.g. Anthropic).
func MessagesHostedSearchTool(typeName string, allowedCallers ...string) messages.ToolTransformer {
	return func(_ messages.ToolLoweringContext, tool canonical.ToolDeclaration) (messages.ToolProjection, []compat.Change, error) {
		if typeName == "" {
			return messages.ToolProjection{}, []compat.Change{{Capability: canonical.RequestToolsKind, Occurrence: canonical.ToolOccurrence(tool.Key()), Kind: compat.Omission}}, nil
		}
		toolDTO := messages.ProviderRequestTool{
			Type: typeName,
			Name: canonical.WebSearchToolKey().Name(),
		}
		if len(allowedCallers) > 0 {
			toolDTO.AllowedCallers = allowedCallers
		}
		return messages.HostedSearchProjection(toolDTO), nil, nil
	}
}

// Re-exports of message-level chatcompletions symbols for provider packages.
type ChatMessageLoweringRule = chatcompletions.MessageLoweringRule
type ChatProviderRequestMessage = chatcompletions.ProviderRequestMessage
