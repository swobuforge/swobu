package state

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func TestReduce_CoreSignalActionRecursesIntoInnerAction(t *testing.T) {
	t.Parallel()

	model := Model{}
	effects := Reduce(&model, update.CoreSignalAction{
		Signal: core.Signal{Kind: "cockpit.test", Data: SetCreateDraftName{Name: "jobs"}},
	})

	if got := model.CreateDraftName; got != "jobs" {
		t.Fatalf("create draft name = %q, want jobs", got)
	}
	if effects != nil {
		t.Fatalf("effects = %#v, want nil", effects)
	}
}
