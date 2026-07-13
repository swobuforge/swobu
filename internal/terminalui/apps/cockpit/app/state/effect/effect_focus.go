package effect

import (
	"context"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
)

// FocusNextAfterRebuildEffect emits a focus-move signal after the next
// render pass so that a rebuilt view tree can land the cursor on the
// correct focus target.
type FocusNextAfterRebuildEffect struct{}

func (eff FocusNextAfterRebuildEffect) Run(ctx context.Context) any {
	_ = ctx
	return interaction.FocusMoveAction{Move: interaction.FocusMoveNext}
}
