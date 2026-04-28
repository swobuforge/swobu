package tui_test

import (
	"strings"
	"testing"
)

func TestSectionTitleGrammar_OpenMovesFocusToFirstChild_CloseKeepsTitleFocus(t *testing.T) {
	daemon := startDaemonWithOneCustomEndpoint(t, "acme")
	journey := startJourneyWithDaemon(t, daemon.BaseURL, 160, 50)

	journey.FocusRow("workspace")
	journey.ActivateFocusedRow() // close workspace
	journey.WaitVisible("↵ open")
	journey.ActivateFocusedRow() // open workspace
	journey.WaitVisible(">   name")

	journey.FocusRow("routing")
	journey.ActivateFocusedRow() // open routing
	journey.WaitVisible(">   run on")
	journey.FocusRow("routing")
	journey.ActivateFocusedRow() // close routing
	journey.WaitVisible("↵ open")

	journey.FocusRow("clients")
	journey.ActivateFocusedRow() // open clients
	journey.WaitVisible(">   client")
	journey.FocusRow("clients")
	journey.ActivateFocusedRow() // close clients
	journey.WaitVisible("↵ open")
}

func TestSectionTitleGrammar_TrafficOpenMovesToFirstTrafficRow(t *testing.T) {
	journey := givenTrafficSeededJourney(t, viewportSize{cols: 160, rows: 50})
	journey.WaitVisibleAny("[› acme]", "[› staging]")
	journey.FocusRowDown("traffic")
	journey.ActivateFocusedRow()
	journey.WaitVisible("chat")
	visible := journey.VisibleOutput()
	if !strings.Contains(visible, "chat") || !strings.Contains(visible, ">   ") {
		t.Fatalf("traffic open did not move focus to first child row; visible=%q", visible)
	}
}
