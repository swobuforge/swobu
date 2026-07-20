package exchange

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/provider"
)

// RuntimeLimits is the one resolved exchange resource policy. Provider media
// code receives only Media; ingress and session checkpoints retain their own lifecycle
// limits without making provider the owning package.
type RuntimeLimits struct {
	MaxRequestBytes    int64
	Media              provider.MediaLimits
	MaxCheckpointBytes int64
}

func (l RuntimeLimits) Validate() error {
	if l.MaxRequestBytes <= 0 || l.MaxCheckpointBytes <= 0 {
		return fmt.Errorf("exchange runtime limits must be positive")
	}
	if err := l.Media.Validate(); err != nil {
		return err
	}
	if l.Media.MaxTotalImageBytes >= l.MaxCheckpointBytes {
		return fmt.Errorf("aggregate image limit leaves no checkpoint structural budget")
	}
	return nil
}

func DefaultRuntimeLimits() RuntimeLimits {
	return RuntimeLimits{MaxRequestBytes: 48 << 20, Media: provider.DefaultMediaLimits(), MaxCheckpointBytes: 40 << 20}
}
