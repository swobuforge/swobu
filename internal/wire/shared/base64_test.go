package shared

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeBase64LimitedRejectsByEncodedLengthBeforeDecode(t *testing.T) {
	const limit = 3
	encoded := strings.Repeat("!", base64.StdEncoding.EncodedLen(limit)+1)

	_, err := DecodeBase64Limited(encoded, limit)
	if !errors.Is(err, ErrBase64DecodedLimit) {
		t.Fatalf("error = %v, want ErrBase64DecodedLimit", err)
	}
}

func TestDecodeBase64LimitedAcceptsValueAtLimit(t *testing.T) {
	want := []byte("abc")
	got, err := DecodeBase64Limited(base64.StdEncoding.EncodeToString(want), len(want))
	if err != nil {
		t.Fatalf("DecodeBase64Limited returned error: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("decoded = %q, want %q", got, want)
	}
}

func TestValidateImageDecodeLimitsCountsImagesAndAggregateInlineBytes(t *testing.T) {
	image, err := canonical.NewInlineImage(canonical.ImageMediaPNG, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewImageMessagePart(image), canonical.NewImageMessagePart(image),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateImageDecodeLimits([]canonical.CanonicalItem{message}, ImageDecodeLimitPolicy{MaxImages: 1}); err == nil {
		t.Fatal("image count limit was not enforced")
	}
	if err := ValidateImageDecodeLimits([]canonical.CanonicalItem{message}, ImageDecodeLimitPolicy{MaxTotalImageBytes: 15}); err == nil {
		t.Fatal("aggregate inline byte limit was not enforced")
	}
}
