package tui_test

import "testing"

func TestCockpitWireframeParity_FooterReflectsFooterVerbInNAV(t *testing.T) {
	daemon := startDaemonWithOneCustomEndpoint(t, "acme")
	journey := startJourneyWithDaemonAndWorkspaceRail(t, daemon.BaseURL, 160, 50, "[› acme]")

	journey.FocusRowDown("name")
	journey.WaitVisible("edit ↵")

	journey.FocusRow("endpoint")
	journey.WaitVisible("copy ↵")

	journey.FocusRow("routing")
	journey.ActivateFocusedRow()
	journey.WaitVisible("run on")
	journey.FocusRow("run on")
	journey.WaitVisible("choose ↵")

	journey.FocusRow("models")
	journey.WaitVisible("manage ↵")

	journey.FocusRow("clients")
	journey.ActivateFocusedRow()
	journey.WaitVisible("setup")
	journey.FocusRow("setup")
	journey.WaitVisible("view ↵")
}
