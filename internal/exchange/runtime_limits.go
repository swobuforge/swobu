package exchange

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/provider"
)

// RuntimeLimits is the one resolved exchange resource policy. Provider media
// code receives only Media; ingress and replay retain their own lifecycle
// limits without making provider the owning package.
type RuntimeLimits struct {
	MaxRequestBytes int64
	Media           provider.MediaLimits
	MaxReplayBytes  int64
}

func (l RuntimeLimits) Validate() error {
	if l.MaxRequestBytes <= 0 || l.MaxReplayBytes <= 0 {
		return fmt.Errorf("exchange runtime limits must be positive")
	}
	if err := l.Media.Validate(); err != nil {
		return err
	}
	if l.Media.MaxTotalImageBytes >= l.MaxReplayBytes {
		return fmt.Errorf("aggregate image limit leaves no replay structural budget")
	}
	return nil
}

func DefaultRuntimeLimits() RuntimeLimits {
	return RuntimeLimits{MaxRequestBytes: 48 << 20, Media: provider.DefaultMediaLimits(), MaxReplayBytes: 40 << 20}
}
