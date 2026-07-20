package shared

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ImageDecodeLimitPolicy is ingress-owned allocation policy supplied to image wire
// decoders. Canonical image construction has no operational byte limit.
type ImageDecodeLimitPolicy struct {
	MaxInlineBytes     int
	MaxImages          int
	MaxTotalImageBytes int
}

// ValidateImageDecodeLimits applies request-scoped operational policy after
// individual inline payloads have already been decoded with a pre-allocation
// bound. Non-positive aggregate limits are left unspecified for callers that
// only need the per-image bound.
func ValidateImageDecodeLimits(items []canonical.CanonicalItem, limits ImageDecodeLimitPolicy) error {
	images := 0
	totalBytes := 0
	count := func(image canonical.ImagePart) error {
		images++
		if inline, ok := image.Source().Inline(); ok {
			totalBytes += len(inline.Data())
		}
		if limits.MaxImages > 0 && images > limits.MaxImages {
			return fmt.Errorf("image count exceeds request limit")
		}
		if limits.MaxTotalImageBytes > 0 && totalBytes > limits.MaxTotalImageBytes {
			return fmt.Errorf("inline image bytes exceed request limit")
		}
		return nil
	}
	for _, item := range items {
		if message, ok := item.Message(); ok {
			for _, part := range message.Content() {
				if image, ok := part.Image(); ok {
					if err := count(image); err != nil {
						return err
					}
				}
			}
		}
		if result, ok := item.ToolResult(); ok {
			for _, part := range result.Content() {
				if image, ok := part.Image(); ok {
					if err := count(image); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// NormalizeImageMediaType maps recognized wire aliases into the closed
// canonical portable media vocabulary.
// swobu:lint ignore string-switch because=image media type is a provider-wire boundary value.
func NormalizeImageMediaType(raw string) (canonical.ImageMediaType, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) { // swobu:io-string source=provider-wire
	case "image/jpeg", "image/jpg":
		return canonical.ImageMediaJPEG, nil
	case "image/png":
		return canonical.ImageMediaPNG, nil
	case "image/webp":
		return canonical.ImageMediaWebP, nil
	case "image/gif":
		return canonical.ImageMediaGIF, nil
	default:
		return "", fmt.Errorf("unsupported image media type")
	}
}
