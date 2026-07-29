package canonical

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

func TestCanonicalInlineImageDoesNotImposeOperationalSizePolicy(t *testing.T) {
	data := make([]byte, (1<<20)+1)
	copy(data, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	image, err := NewInlineImage(ImageMediaPNG, data, Unspecified[ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	media, _ := image.Source().Inline()
	if len(media.Data()) != len(data) {
		t.Fatalf("inline image bytes = %d, want %d", len(media.Data()), len(data))
	}
}

func TestCanonicalRejectsWireAutoAsExplicitDetail(t *testing.T) {
	if _, err := NewURLImage("https://example.test/image.png", Specify(ImageDetail("auto"))); err == nil {
		t.Fatal("canonical accepted explicit auto detail")
	}
}

func TestCanonicalImagePlacementAllowsConversationMessagesAndToolResults(t *testing.T) {
	image, err := NewInlineImage(ImageMediaPNG, pngSignature(), Unspecified[ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []MessageRole{MessageRoleUser, MessageRoleAssistant} {
		if _, err := NewMessageItem(role, []MessagePart{NewImageMessagePart(image)}); err != nil {
			t.Fatalf("canonical rejected image in %s message: %v", role, err)
		}
	}
	for _, role := range []MessageRole{MessageRoleSystem, MessageRoleDeveloper} {
		if _, err := NewMessageItem(role, []MessagePart{NewImageMessagePart(image)}); err == nil {
			t.Fatalf("canonical accepted image in %s message", role)
		}
	}
	callID, _ := NewToolCallID("call_image")
	if _, err := NewToolResultItem(callID, []ToolResultPart{NewImageToolResultPart(image)}, false); err != nil {
		t.Fatalf("canonical rejected tool-result image: %v", err)
	}
}

func TestCanonicalImageDetailOriginalIsPreserved(t *testing.T) {
	image, err := NewInlineImage(ImageMediaPNG, pngSignature(), Specify(ImageDetailOriginal))
	if err != nil {
		t.Fatal(err)
	}
	detail, ok := image.Detail().Get()
	if !ok || detail != ImageDetailOriginal {
		t.Fatalf("detail = %q specified=%v, want original", detail, ok)
	}
}

func TestCanonicalURLImagePreservesSpellingAndRejectsCredentialsOrPadding(t *testing.T) {
	raw := "https://example.test/%7epath?b=2&a=1"
	image, err := NewURLImage(raw, Unspecified[ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	urlImage, ok := image.Source().URL()
	if !ok || urlImage.String() != raw {
		t.Fatalf("URL = %q, want exact %q", urlImage.String(), raw)
	}
	for _, invalid := range []string{" " + raw, raw + " ", "https://user:secret@example.test/image.png", raw + "#frag"} {
		if _, err := NewURLImage(invalid, Unspecified[ImageDetail]()); err == nil {
			t.Fatalf("invalid URL %q accepted", invalid)
		}
	}
}

func TestCanonicalInlineImageKeepsValidationOperationallyCheap(t *testing.T) {
	if _, err := NewInlineImage(ImageMediaPNG, []byte("owned-but-not-decoded-here"), Unspecified[ImageDetail]()); err != nil {
		t.Fatalf("canonical rejected non-empty owned bytes: %v", err)
	}
	if _, err := NewInlineImage(ImageMediaType("image/unknown"), []byte("bytes"), Unspecified[ImageDetail]()); err == nil {
		t.Fatal("canonical accepted unsupported media declaration")
	}
}

func TestCanonicalInlineImageOwnsAndClonesBytes(t *testing.T) {
	data := pngSignature()
	image, err := NewInlineImage(ImageMediaPNG, data, Unspecified[ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 0
	first, _ := image.Source().Inline()
	clone := image.Clone()
	cloneBytes, _ := clone.Source().Inline()
	got := cloneBytes.Data()
	got[1] = 0
	if first.Data()[0] != 0x89 || cloneBytes.Data()[1] != 'P' {
		t.Fatal("inline image bytes alias caller or accessor storage")
	}
}

func pngSignature() []byte {
	return []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
}

func encodedGIF(t *testing.T, frames int) []byte {
	t.Helper()
	palette := color.Palette{color.Black, color.White}
	images := make([]*image.Paletted, frames)
	for i := range images {
		images[i] = image.NewPaletted(image.Rect(0, 0, 1, 1), palette)
	}
	var out bytes.Buffer
	if err := gif.EncodeAll(&out, &gif.GIF{Image: images, Delay: make([]int, frames)}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
