package session

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestResolvedMediaBindsOccurrencesAndDeduplicatesOwnedAssets(t *testing.T) {
	source := []byte("exact")
	media, err := (ResolvedMedia{}).Bind(canonical.RequestPartRef{Item: 2, Part: 1}, "https://example.test/image.png", canonical.ImageMediaPNG, source)
	if err != nil {
		t.Fatal(err)
	}
	media, err = media.Bind(canonical.RequestPartRef{Item: 4, Part: 0}, "https://example.test/image.png", canonical.ImageMediaPNG, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := media.Bind(canonical.RequestPartRef{Item: 4, Part: 0}, "https://example.test/other.png", canonical.ImageMediaPNG, source); err == nil {
		t.Fatal("duplicate occurrence binding was accepted")
	}
	source[0] = 'X'
	if media.AssetCount() != 1 || media.BindingCount() != 2 || string(media.assets[0].bytes) != "exact" {
		t.Fatalf("normalized media = %#v", media)
	}
	clone := media.Clone()
	clone.assets[0].bytes[0] = 'Y'
	if string(media.assets[0].bytes) != "exact" {
		t.Fatal("clone aliased source bytes")
	}
}

func TestResolvedMediaBindRejectsInvalidSourceAndMediaType(t *testing.T) {
	if _, err := (ResolvedMedia{}).Bind(canonical.RequestPartRef{}, "file:///tmp/image.png", canonical.ImageMediaPNG, []byte("bytes")); err == nil {
		t.Fatal("non-HTTP media source was accepted")
	}
	if _, err := (ResolvedMedia{}).Bind(canonical.RequestPartRef{}, "https://example.test/image.png", canonical.ImageMediaType("image/avif"), []byte("bytes")); err == nil {
		t.Fatal("non-portable media type was accepted")
	}
}

func TestResolvedMediaValidateForRequestRequiresExactURLOccurrenceCoverage(t *testing.T) {
	image, err := canonical.NewURLImage("https://example.test/image.png", canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewTextMessagePart("see"),
		canonical.NewImageMessagePart(image),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{message}})

	if err := (ResolvedMedia{}).ValidateForRequest(request); err == nil {
		t.Fatal("URL image without a resolved occurrence binding was accepted")
	}
	media, err := (ResolvedMedia{}).Bind(canonical.RequestPartRef{Part: 1}, "https://example.test/image.png", canonical.ImageMediaPNG, []byte("bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if err := media.ValidateForRequest(request); err != nil {
		t.Fatalf("complete occurrence binding rejected: %v", err)
	}
}
