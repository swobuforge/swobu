package effect

import (
	"context"
	"time"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// FocusNextAfterRebuildEffect defers one focus-next hop so newly opened
// section children are present in the rebuilt tree before focus traversal.
type FocusNextAfterRebuildEffect struct {
	Delay time.Duration
}

func (eff FocusNextAfterRebuildEffect) Execute(ctx context.Context) []update.Action {
	delay := eff.Delay
	if delay <= 0 {
		delay = 2 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil
	case <-timer.C:
		return []update.Action{interaction.FocusMoveAction{Move: interaction.FocusMoveNext}}
	}
}
