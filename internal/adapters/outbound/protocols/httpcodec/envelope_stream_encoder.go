package httpcodec

import "github.com/swobuforge/swobu/internal/domain/canonical"

// EnvelopeStreamEncoder maps canonical envelope events into protocol-native
// streaming frames.
type EnvelopeStreamEncoder interface {
	EncodeEnvelopeEvent(event canonical.Event) ([][]byte, error)
	Finish() ([][]byte, error)
}
