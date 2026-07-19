package responses

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeOutputItems_AcceptsMcpCallAsToolUse(t *testing.T) {
	t.Parallel()

	items, err := decodeOutputItems(context.Background(), []responsesWireOutputItemDTO{
		{
			Type:      "mcp_call",
			ID:        "mcp_1",
			Name:      "Read",
			Arguments: `{"path":"workspace/file.txt"}`,
		},
	}, "", "ex_mcp", nil)
	if err != nil {
		t.Fatalf("decodeOutputItems returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("output item count = %d, want 1", len(items))
	}
	item := items[0]
	if item.Kind() != canonical.ItemKindToolUse {
		t.Fatalf("item kind = %s, want %s", item.Kind(), canonical.ItemKindToolUse)
	}
	if item.ItemID() != "mcp_1" {
		t.Fatalf("item id = %q, want mcp_1", item.ItemID())
	}
	toolUse, _ := item.ToolUse()
	if toolUse.UseID != "mcp_1" {
		t.Fatalf("tool use id = %q, want mcp_1", toolUse.UseID)
	}
	if toolUse.Name != "Read" {
		t.Fatalf("item name = %q, want Read", toolUse.Name)
	}
	if got := toolUse.Input.RawObject(); got != `{"path":"workspace/file.txt"}` {
		t.Fatalf("arguments = %s, want normalized path JSON", got)
	}
}

func TestDecodeResponseBuffered_AcceptsMcpCall(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"id":"resp_1",
		"model":"m",
		"output":[{"type":"mcp_call","id":"mcp_1","name":"Read","arguments":"{\"path\":\"workspace/file.txt\"}"}]
	}`)

	reader, err := decodeResponseBuffered(context.Background(), raw, "ex_mcp", nil)
	if err != nil {
		t.Fatalf("decodeResponseBuffered returned error: %v", err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), reader, canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	out, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	items := out.Items()
	if len(items) != 1 {
		t.Fatalf("output item count = %d, want 1", len(items))
	}
	item := items[0]
	if item.Kind() != canonical.ItemKindToolUse {
		t.Fatalf("item kind = %s, want %s", item.Kind(), canonical.ItemKindToolUse)
	}
	toolUse, _ := item.ToolUse()
	if toolUse.UseID != "mcp_1" {
		t.Fatalf("tool use id = %q, want mcp_1", toolUse.UseID)
	}
	if toolUse.Name != "Read" {
		t.Fatalf("item name = %q, want Read", toolUse.Name)
	}
}
