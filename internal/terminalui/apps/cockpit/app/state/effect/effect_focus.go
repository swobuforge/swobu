package effect

import (
	"context"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// FocusNextAfterRebuildEffect emits one focus-next hop after the current
// rebuild has committed the newly opened subtree.
//
// The loop executes this follow-up inline in the same update cycle so the
// post-open focus handoff is deterministic.
type FocusNextAfterRebuildEffect struct{}

func (FocusNextAfterRebuildEffect) RunImmediately() {}

func (eff FocusNextAfterRebuildEffect) Execute(ctx context.Context) []update.Action {
	select {
	case <-ctx.Done():
		return nil
	default:
		return []update.Action{interaction.FocusMoveAction{Move: interaction.FocusMoveNext}}
	}
}
