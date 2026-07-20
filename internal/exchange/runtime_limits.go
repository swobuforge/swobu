package exchange

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/provider"
)

// RuntimeLimits is the one resolved exchange resource policy. Request bytes
// and admitted media are bounded at their real ingress/allocation boundaries.
type RuntimeLimits struct {
	MaxRequestBytes int64
	Media           provider.MediaLimits
}

func (l RuntimeLimits) Validate() error {
	if l.MaxRequestBytes <= 0 {
		return fmt.Errorf("exchange runtime limits must be positive")
	}
	if err := l.Media.Validate(); err != nil {
		return err
	}
	return nil
}

func DefaultRuntimeLimits() RuntimeLimits {
	return RuntimeLimits{MaxRequestBytes: 48 << 20, Media: provider.DefaultMediaLimits()}
}
