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
