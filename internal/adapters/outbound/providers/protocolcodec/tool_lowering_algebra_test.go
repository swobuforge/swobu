package protocolcodec

import (
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
	"github.com/swobuforge/swobu/internal/wire/messages"
	"github.com/swobuforge/swobu/internal/wire/responses"
)

func TestToolLoweringAlgebraIsTotalAndSparseAcrossWireFamilies(t *testing.T) {
	t.Run("Chat Completions", func(t *testing.T) {
		baseline := chatcompletions.DefaultToolLowering()
		override := func(chatcompletions.ToolLoweringContext, canonical.ToolDeclaration) (chatcompletions.ToolProjection, []compat.Change, error) {
			return chatcompletions.ToolProjection{}, nil, nil
		}
		resolved := baseline.Overlay(chatcompletions.ToolLowering{Custom: override})
		assertTotalSlots(t, baseline.Function, baseline.Custom, baseline.WebSearch, baseline.Discovery)
		assertSparseOverlay(t,
			[]any{baseline.Function, baseline.Custom, baseline.WebSearch, baseline.Discovery},
			[]any{resolved.Function, resolved.Custom, resolved.WebSearch, resolved.Discovery},
			1,
		)
		if functionPointer(resolved.Custom) != functionPointer(override) {
			t.Fatal("Chat Custom override was not installed")
		}
	})

	t.Run("Responses", func(t *testing.T) {
		baseline := responses.DefaultToolLowering()
		override := func(responses.ToolLoweringContext, canonical.ToolDeclaration) (responses.ToolProjection, []compat.Change, error) {
			return responses.ToolProjection{TargetType: "provider_search_call"}, nil, nil
		}
		resolved := baseline.Overlay(responses.ToolLowering{WebSearch: override})
		assertTotalSlots(t, baseline.Function, baseline.Custom, baseline.WebSearch, baseline.Discovery)
		assertSparseOverlay(t,
			[]any{baseline.Function, baseline.Custom, baseline.WebSearch, baseline.Discovery},
			[]any{resolved.Function, resolved.Custom, resolved.WebSearch, resolved.Discovery},
			2,
		)
		if functionPointer(resolved.WebSearch) != functionPointer(override) {
			t.Fatal("Responses WebSearch override was not installed")
		}
	})

	t.Run("Messages", func(t *testing.T) {
		baseline := messages.DefaultToolLowering()
		override := func(messages.ToolLoweringContext, canonical.ToolDeclaration) (messages.ToolProjection, []compat.Change, error) {
			return messages.ToolProjection{TargetType: "provider_discovery_call"}, nil, nil
		}
		resolved := baseline.Overlay(messages.ToolLowering{Discovery: override})
		assertTotalSlots(t, baseline.Function, baseline.Custom, baseline.WebSearch, baseline.Discovery)
		assertSparseOverlay(t,
			[]any{baseline.Function, baseline.Custom, baseline.WebSearch, baseline.Discovery},
			[]any{resolved.Function, resolved.Custom, resolved.WebSearch, resolved.Discovery},
			3,
		)
		if functionPointer(resolved.Discovery) != functionPointer(override) {
			t.Fatal("Messages Discovery override was not installed")
		}
	})
}

func TestSemanticLoweringOverlayComposesProtocolAndIndependentProviderSlots(t *testing.T) {
	chatProtocolReasoning := func(canonical.CanonicalRequest, chatcompletions.ReasoningTargetDialect, *[]compat.Change, string) (map[string]any, error) {
		return map[string]any{"protocol": true}, nil
	}
	chatProviderMessage := func(*chatcompletions.ProviderRequestMessage, []canonical.CanonicalItem) error { return nil }
	chatProviderCustom := func(chatcompletions.ToolLoweringContext, canonical.ToolDeclaration) (chatcompletions.ToolProjection, []compat.Change, error) {
		return chatcompletions.ToolProjection{}, nil, nil
	}
	chatProviderSearch := func(chatcompletions.ToolLoweringContext, canonical.ToolDeclaration) (chatcompletions.ToolProjection, []compat.Change, error) {
		return chatcompletions.ToolProjection{}, nil, nil
	}
	chatDefault := chatcompletions.DefaultLowering()
	chatProtocol := chatDefault.Overlay(chatcompletions.Lowering{Reasoning: chatProtocolReasoning})
	chatResolved := chatProtocol.Overlay(chatcompletions.Lowering{
		Tools:   chatcompletions.ToolLowering{Custom: chatProviderCustom, WebSearch: chatProviderSearch},
		Message: chatProviderMessage,
	})
	assertTotalSlots(t,
		chatResolved.Tools.Function, chatResolved.Tools.Custom, chatResolved.Tools.WebSearch, chatResolved.Tools.Discovery,
		chatResolved.Reasoning, chatResolved.Message,
	)
	if functionPointer(chatResolved.Reasoning) != functionPointer(chatProtocolReasoning) ||
		functionPointer(chatResolved.Message) != functionPointer(chatProviderMessage) ||
		functionPointer(chatResolved.Tools.Custom) != functionPointer(chatProviderCustom) ||
		functionPointer(chatResolved.Tools.WebSearch) != functionPointer(chatProviderSearch) ||
		functionPointer(chatResolved.Tools.Function) != functionPointer(chatDefault.Tools.Function) ||
		functionPointer(chatResolved.Tools.Discovery) != functionPointer(chatDefault.Tools.Discovery) {
		t.Fatal("Chat default -> protocol -> provider overlay did not preserve independent slot ownership")
	}

	messagesProtocolReasoning := func(map[string]any, canonical.ReasoningControls, *[]compat.Change) error { return nil }
	messagesProviderCustom := func(messages.ToolLoweringContext, canonical.ToolDeclaration) (messages.ToolProjection, []compat.Change, error) {
		return messages.ToolProjection{}, nil, nil
	}
	messagesProviderSearch := func(messages.ToolLoweringContext, canonical.ToolDeclaration) (messages.ToolProjection, []compat.Change, error) {
		return messages.ToolProjection{TargetType: "provider_search_call"}, nil, nil
	}
	messagesDefault := messages.DefaultLowering()
	messagesProtocol := messagesDefault.Overlay(messages.Lowering{Reasoning: messagesProtocolReasoning})
	messagesResolved := messagesProtocol.Overlay(messages.Lowering{Tools: messages.ToolLowering{
		Custom:    messagesProviderCustom,
		WebSearch: messagesProviderSearch,
	}})
	assertTotalSlots(t,
		messagesResolved.Tools.Function, messagesResolved.Tools.Custom, messagesResolved.Tools.WebSearch, messagesResolved.Tools.Discovery,
		messagesResolved.Reasoning,
	)
	if functionPointer(messagesResolved.Reasoning) != functionPointer(messagesProtocolReasoning) ||
		functionPointer(messagesResolved.Tools.Custom) != functionPointer(messagesProviderCustom) ||
		functionPointer(messagesResolved.Tools.WebSearch) != functionPointer(messagesProviderSearch) ||
		functionPointer(messagesResolved.Tools.Function) != functionPointer(messagesDefault.Tools.Function) ||
		functionPointer(messagesResolved.Tools.Discovery) != functionPointer(messagesDefault.Tools.Discovery) {
		t.Fatal("Messages default -> protocol -> provider overlay did not preserve independent slot ownership")
	}
}

func assertTotalSlots(t *testing.T, slots ...any) {
	t.Helper()
	for index, slot := range slots {
		if functionPointer(slot) == 0 {
			t.Fatalf("slot %d is unresolved", index)
		}
	}
}

func assertSparseOverlay(t *testing.T, baseline, resolved []any, changed int) {
	t.Helper()
	for index := range baseline {
		if index == changed {
			continue
		}
		if functionPointer(baseline[index]) != functionPointer(resolved[index]) {
			t.Fatalf("overlay changed unrelated slot %d", index)
		}
	}
}

func functionPointer(function any) uintptr {
	if function == nil {
		return 0
	}
	return reflect.ValueOf(function).Pointer()
}
