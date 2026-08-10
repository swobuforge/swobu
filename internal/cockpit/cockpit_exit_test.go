package cockpit

import (
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestCockpit_CtrlCStopsApp(t *testing.T) {
	cockpit := NewCockpit(readmodel.CockpitReadModel{})
	h, err := testkit.NewHarnessAt(cockpit, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'c', Mod: tui.ModCtrl})

	select {
	case <-h.App().StopCh():
	default:
		t.Fatal("Ctrl-C did not stop Cockpit")
	}
}
