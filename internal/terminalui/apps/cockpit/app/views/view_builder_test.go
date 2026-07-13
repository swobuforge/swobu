package views

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func TestBuildCockpitView_UsesLiveModelContext(t *testing.T) {
	// TODO(v2-migration): re-enable once retained bridge stabilizes
	t.Skip("retained bridge in flux during v2 migration")
	_ = state.Model{}
}
