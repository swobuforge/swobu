package compat

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestValidateFeature(t *testing.T) {
	t.Parallel()

	valid := []Feature{
		RequestToolChoice,
		ToolNameNamespace,
		GenerationStopSequences,
		UsageCacheReadTokens,
		WireNativePayload,
	}
	for _, feature := range valid {
		if err := ValidateFeature(feature); err != nil {
			t.Fatalf("ValidateFeature(%q) = %v", feature, err)
		}
	}

	invalid := []Feature{
		Feature("RequestFeature"),
		Feature("tool.name.namespace"),
		Feature("tool.name.flattened"),
		Feature("openai.tool_choice"),
		Feature("usage.missing"),
		Feature("responses.reasoning"),
	}
	for _, feature := range invalid {
		if err := ValidateFeature(feature); err == nil {
			t.Fatalf("ValidateFeature(%q) expected error", feature)
		}
	}
}

func TestValidateSubject(t *testing.T) {
	t.Parallel()

	valid := []Subject{
		"wire:/messages/2/tool_calls/0/id",
		"wire:/input/0/call_id",
		"canonical:items[3].tool_call.id",
		"state:turn.request.raw",
		"route:provider/openrouter/protocol/chat_completions",
	}
	for _, subject := range valid {
		if err := ValidateSubject(subject); err != nil {
			t.Fatalf("ValidateSubject(%q) = %v", subject, err)
		}
	}

	invalid := []Subject{
		"messages[2].tool_calls[0].id",
		"because missing id",
		"wire:messages.2.id",
	}
	for _, subject := range invalid {
		if err := ValidateSubject(subject); err == nil {
			t.Fatalf("ValidateSubject(%q) expected error", subject)
		}
	}
}

func TestSupportsFeature(t *testing.T) {
	t.Parallel()

	if got := SupportsFeature("openai", protocolkind.Responses, "", ToolDeclaration); got != Supported {
		t.Fatalf("openai responses tool declaration support = %q want %q", got, Supported)
	}
	if got := SupportsFeature("openai", protocolkind.ChatCompletions, "", GenerationStopSequences); got != Supported {
		t.Fatalf("openai chat_completions stop sequence support = %q want %q", got, Supported)
	}
	if got := SupportsFeature("anthropic", protocolkind.Messages, "", RequestStructuredOutput); got != Unsupported {
		t.Fatalf("anthropic messages structured output support = %q want %q", got, Unsupported)
	}
	if got := SupportsFeature("azure", protocolkind.Messages, "claude-haiku-4-5-20251001", ToolDeclaration); got != Supported {
		t.Fatalf("azure claude messages tool declaration support = %q want %q", got, Supported)
	}
	if got := SupportsFeature("ollama", protocolkind.Responses, "", ToolDeclaration); got != Unknown {
		t.Fatalf("ollama responses support = %q want %q", got, Unknown)
	}
	if got := SupportsFeature("ollama", protocolkind.ChatCompletions, "", ToolDeclaration); got != Supported {
		t.Fatalf("ollama chat_completions support = %q want %q", got, Supported)
	}
	if got := SupportsFeature("missing", protocolkind.Responses, "", ToolDeclaration); got != Unknown {
		t.Fatalf("unknown route support = %q want %q", got, Unknown)
	}
	for _, feature := range featureSetMessages {
		if got := SupportsFeature("openai_compatible", protocolkind.Messages, "glm-5.2", feature); got != Supported {
			t.Fatalf("custom messages feature %q support = %q want %q", feature, got, Supported)
		}
	}
	if got := SupportsFeature("openai_compatible", protocolkind.Messages, "glm-5.2", RequestStructuredOutput); got != Unsupported {
		t.Fatalf("custom messages structured output support = %q want %q", got, Unsupported)
	}
}

func TestCapabilitiesForRoute(t *testing.T) {
	t.Parallel()

	caps := CapabilitiesForRoute("openai", "responses", "")
	if len(caps) == 0 {
		t.Fatal("openai responses capabilities should not be empty")
	}
	found := false
	for _, cap := range caps {
		if cap.Feature == ToolDeclaration {
			found = true
			if cap.Support != Supported {
				t.Fatalf("tool declaration support = %q want %q", cap.Support, Supported)
			}
		}
	}
	if !found {
		t.Fatal("openai responses capabilities missing tool declaration support")
	}
	if got := CapabilitiesForRoute("missing", "responses", ""); got != nil {
		t.Fatalf("unknown route capabilities = %v want nil", got)
	}
}
