package chatcompletions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestCustomToolResponseRoundTrip(t *testing.T) {
	t.Parallel()

	output := canonical.NewConversationOutput(
		"resp_1",
		"gpt-4.1-mini",
		[]canonical.CanonicalItem{
			canonical.NewCustomToolUseOutputItem("custom_1", "call_1", "apply_patch", canonical.NewToolArgumentsObject("patch contents")),
		},
		"stop",
	)

	wire, err := (legacyResponseDocumentEncoder{}).EncodeResponseDocument(output)
	if err != nil {
		t.Fatalf("EncodeResponseDocument returned error: %v", err)
	}

	var dto chatCompletionsResponseDTO
	if err := json.Unmarshal(wire.Raw, &dto); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if len(dto.Choices) != 1 {
		t.Fatalf("choice count = %d, want 1", len(dto.Choices))
	}
	message := dto.Choices[0].Message
	if len(message.ToolCalls) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(message.ToolCalls))
	}
	call := message.ToolCalls[0]
	if call.Type != "custom" || call.Custom == nil {
		t.Fatalf("tool call = %#v, want custom tool call", call)
	}
	if call.Custom.Name != "apply_patch" {
		t.Fatalf("custom name = %q, want apply_patch", call.Custom.Name)
	}
	if call.Custom.Input != "patch contents" {
		t.Fatalf("custom input = %q, want patch contents", call.Custom.Input)
	}

	reader, err := decodeResponseBuffered(context.Background(), wire.Raw, "ex_custom", nil)
	if err != nil {
		t.Fatalf("decodeResponseBuffered returned error: %v", err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), reader, canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	projected, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	items := projected.Items()
	if len(items) != 1 {
		t.Fatalf("item count = %d, want 1", len(items))
	}
	item := items[0]
	if item.Kind() != canonical.ItemKindToolUse {
		t.Fatalf("item kind = %s, want %s", item.Kind(), canonical.ItemKindToolUse)
	}
	toolUse, _ := item.ToolUse()
	if toolUse.ToolType != canonical.ToolTypeCustom {
		t.Fatalf("tool type = %q, want %q", toolUse.ToolType, canonical.ToolTypeCustom)
	}
	if toolUse.Name != "apply_patch" {
		t.Fatalf("item name = %q, want apply_patch", toolUse.Name)
	}
	if got := toolUse.Input.RawObject(); got != "patch contents" {
		t.Fatalf("item input = %q, want patch contents", got)
	}
}
