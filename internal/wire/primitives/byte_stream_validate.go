package core

import (
	"fmt"
	"mime"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
)

// ValidateResponseSSEByteStream validates one carrier-native response stream contract.
func ValidateResponseSSEByteStream(stream carrier.ByteStream) error {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(stream.MediaType))
	if err != nil || !strings.EqualFold(mediaType, "text/event-stream") {
		return fmt.Errorf("wire stream media type must be %q", "text/event-stream")
	}
	if stream.Body == nil {
		return fmt.Errorf("wire stream body must be configured")
	}
	return nil
}
