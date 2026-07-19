package openai

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeContentParts_NormalizesStringIntoTextPart(t *testing.T) {
	t.Parallel()

	parts, err := DecodeContentParts(json.RawMessage(`"hello"`), "surface")
	if err != nil {
		t.Fatalf("DecodeContentParts returned error: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("parts len = %d, want 1", len(parts))
	}
	if parts[0].Type != "text" {
		t.Fatalf("part type = %q, want text", parts[0].Type)
	}
	if parts[0].Text != "hello" {
		t.Fatalf("part text = %q, want hello", parts[0].Text)
	}
}

func TestDecodeContentParts_PreservesStructuredPartFields(t *testing.T) {
	t.Parallel()

	parts, err := DecodeContentParts(json.RawMessage(`[
		{"type":"text","text":"working"},
		{"type":"tool_use","id":"tool_1","name":"Read","input":{"path":"file.txt"}}
	]`), "surface")
	if err != nil {
		t.Fatalf("DecodeContentParts returned error: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("parts len = %d, want 2", len(parts))
	}
	if parts[1].Type != "tool_use" {
		t.Fatalf("part type = %q, want tool_use", parts[1].Type)
	}
	if parts[1].ID != "tool_1" {
		t.Fatalf("part id = %q, want tool_1", parts[1].ID)
	}
	if parts[1].Name != "Read" {
		t.Fatalf("part name = %q, want Read", parts[1].Name)
	}
	if got := string(parts[1].Input); got != `{"path":"file.txt"}` {
		t.Fatalf("part input = %s, want {\"path\":\"file.txt\"}", got)
	}
}

func TestWalkContentParts_VisitsInOrder(t *testing.T) {
	t.Parallel()

	parts := []ContentPartItem{
		{Type: "text"},
		{Type: "tool_use"},
	}
	seen := make([]string, 0, len(parts))
	if err := WalkContentParts(parts, func(idx int, part ContentPartItem) error {
		seen = append(seen, fmt.Sprintf("%d:%s", idx, part.Type))
		return nil
	}); err != nil {
		t.Fatalf("WalkContentParts returned error: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("seen len = %d, want 2", len(seen))
	}
	if seen[0] != "0:text" || seen[1] != "1:tool_use" {
		t.Fatalf("seen = %#v, want [0:text 1:tool_use]", seen)
	}
}

func TestDecodeTextContentItems_UsesWalker(t *testing.T) {
	t.Parallel()

	items, err := DecodeTextContentItems(json.RawMessage(`[
		{"type":"input_text","input_text":"hello"},
		{"type":"output_text","text":"world"}
	]`), "surface", canonical.ItemAuthorAssistant)
	if err != nil {
		t.Fatalf("DecodeTextContentItems returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	first, _ := items[0].TextItem()
	second, _ := items[1].TextItem()
	if first.Text != "hello" || second.Text != "world" {
		t.Fatalf("items = %#v", items)
	}
}
