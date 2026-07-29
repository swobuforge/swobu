package openai

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
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
	]`), "surface", canonical.MessageRoleAssistant, shared.ImageDecodeLimitPolicy{MaxInlineBytes: 1024}, nil)
	if err != nil {
		t.Fatalf("DecodeTextContentItems returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	message, _ := items[0].Message()
	parts := message.Content()
	first, _ := parts[0].Text()
	second, _ := parts[1].Text()
	if first.Text() != "hello" || second.Text() != "world" {
		t.Fatalf("items = %#v", items)
	}
}

func TestOpenAIImageDetailSurvivesCanonicalRoundTrip(t *testing.T) {
	image, err := DecodeOpenAIImage(json.RawMessage(`{"url":"https://example.test/image.png","detail":"high"}`), "surface", shared.ImageDecodeLimitPolicy{MaxInlineBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	detail, ok := image.Detail().Get()
	if !ok || detail != canonical.ImageDetailHigh {
		t.Fatalf("detail = %q, want high", detail)
	}
	url, detail, err := EncodeOpenAIImageURL(image)
	if err != nil || url != "https://example.test/image.png" || detail != canonical.ImageDetailHigh {
		t.Fatalf("encoded image = %q detail=%q err=%v", url, detail, err)
	}
}

func TestOpenAIImageAutoCollapsesAndMediaAliasNormalizes(t *testing.T) {
	image, err := DecodeOpenAIImage(json.RawMessage(`{"url":"data:image/jpg;base64,/9j/","detail":"auto"}`), "surface", shared.ImageDecodeLimitPolicy{MaxInlineBytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	if image.Detail().IsSpecified() {
		t.Fatal("wire auto survived as canonical detail")
	}
	inline, ok := image.Source().Inline()
	if !ok || inline.MediaType() != canonical.ImageMediaJPEG {
		t.Fatalf("inline image = %#v, want normalized JPEG", inline)
	}
}

func TestDecodeTextContentItemsRejectsImageOutsideUserRole(t *testing.T) {
	_, err := DecodeTextContentItems(json.RawMessage(`[{"type":"image_url","image_url":{"url":"https://example.test/image.png"}}]`), "surface", canonical.MessageRoleAssistant, shared.ImageDecodeLimitPolicy{MaxInlineBytes: 1024}, nil)
	if err == nil {
		t.Fatal("assistant image input was accepted")
	}
}

func TestDecodeTextContentItemsRejectsProviderFileIDWithoutEchoingIt(t *testing.T) {
	const fileID = "file_secret_123"
	_, err := DecodeTextContentItems(json.RawMessage(`[{"type":"input_image","file_id":"`+fileID+`"}]`), "surface", canonical.MessageRoleUser, shared.ImageDecodeLimitPolicy{MaxInlineBytes: 1024}, nil)
	if err == nil {
		t.Fatal("provider image file ID was accepted")
	}
	if strings.Contains(err.Error(), fileID) {
		t.Fatalf("error echoed provider file ID: %v", err)
	}
}
