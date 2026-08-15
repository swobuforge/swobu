package protocolcodec

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
)

const testProviderChatScope canonical.ProviderChatReplayScope = "test-chat"

func TestProviderChatReplayForMessageAssociatesExactScopeWithinSourceRange(t *testing.T) {
	foreign := testProviderChatReasoning(t, "foreign", "other-chat")
	wanted := testProviderChatReasoning(t, "wanted", testProviderChatScope)
	message := chatcompletions.ProviderRequestMessage{Role: "assistant", SourceStart: 1, SourceEnd: 2}

	raw, ok, err := ProviderChatReplayForMessage(message, []canonical.CanonicalItem{foreign, wanted, testProviderChatReasoning(t, "outside", testProviderChatScope)}, testProviderChatScope)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || string(raw) != "wanted" {
		t.Fatalf("replay = %q/%t, want wanted/true", raw, ok)
	}
}

func TestProviderChatReplayForMessageIgnoresForeignScope(t *testing.T) {
	message := chatcompletions.ProviderRequestMessage{Role: "assistant", SourceStart: 0, SourceEnd: 1}
	raw, ok, err := ProviderChatReplayForMessage(message, []canonical.CanonicalItem{testProviderChatReasoning(t, "foreign", "other-chat")}, testProviderChatScope)
	if err != nil {
		t.Fatal(err)
	}
	if ok || raw != nil {
		t.Fatalf("foreign replay = %q/%t, want nil/false", raw, ok)
	}
}

func TestProviderChatReplayForMessageRejectsDuplicateMatchingScope(t *testing.T) {
	message := chatcompletions.ProviderRequestMessage{Role: "assistant", SourceStart: 0, SourceEnd: 2}
	_, _, err := ProviderChatReplayForMessage(message, []canonical.CanonicalItem{
		testProviderChatReasoning(t, "first", testProviderChatScope),
		testProviderChatReasoning(t, "second", testProviderChatScope),
	}, testProviderChatScope)
	if err == nil || !strings.Contains(err.Error(), "duplicate provider Chat opaque thinking") {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestProviderChatReplayForMessageHonorsAssistantAndRangeBoundaries(t *testing.T) {
	item := testProviderChatReasoning(t, "wanted", testProviderChatScope)
	cases := []struct {
		name    string
		message chatcompletions.ProviderRequestMessage
		items   []canonical.CanonicalItem
		want    bool
	}{
		{name: "non assistant", message: chatcompletions.ProviderRequestMessage{Role: "user", SourceStart: 0, SourceEnd: 1}, items: []canonical.CanonicalItem{item}},
		{name: "negative start", message: chatcompletions.ProviderRequestMessage{Role: "assistant", SourceStart: -1, SourceEnd: 1}, items: []canonical.CanonicalItem{item}},
		{name: "empty range", message: chatcompletions.ProviderRequestMessage{Role: "assistant", SourceStart: 1, SourceEnd: 1}, items: []canonical.CanonicalItem{item}},
		{name: "end clamps", message: chatcompletions.ProviderRequestMessage{Role: "assistant", SourceStart: 0, SourceEnd: 9}, items: []canonical.CanonicalItem{item}, want: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, got, err := ProviderChatReplayForMessage(test.message, test.items, testProviderChatScope)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("matched = %t, want %t", got, test.want)
			}
		})
	}
}

func testProviderChatReasoning(t *testing.T, text string, scope canonical.ProviderChatReplayScope) canonical.CanonicalItem {
	t.Helper()
	opaque, err := canonical.NewProviderChatOpaqueThinking(scope, []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, text)
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	return item
}
