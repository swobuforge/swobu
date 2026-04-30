package tui_test

import "testing"

func TestCockpitInteractionModes_FooterReflectsEditMode(t *testing.T) {
	daemon := startDaemonWithOneCustomEndpoint(t, "acme")
	journey := startJourneyWithDaemonAndWorkspaceRail(t, daemon.BaseURL, 160, 50, "acme")
	journey.WaitVisibleAny("ready", "offline (stale)")
	journey.FocusRow("name")
	journey.ActivateFocusedRow()
	journey.WaitVisible("↵ save")
	journey.WaitVisible("esc close")
}

func TestCockpitInteractionModes_EscClosesRunOnPickerLocally(t *testing.T) {
	daemon := startDaemonWithOneCustomEndpoint(t, "acme")
	journey := startJourneyWithDaemonAndWorkspaceRail(t, daemon.BaseURL, 160, 50, "acme")
	journey.FocusRowDown("routing")
	journey.ActivateFocusedRow()
	journey.WaitVisible("run on")
	journey.FocusRow("run on")
	journey.ActivateFocusedRow()
	journey.WaitVisible("Custom")
	journey.SendKey("esc")
	journey.WaitVisible("esc close")
	journey.AssertVisibleContains("Custom")
}

func TestCockpitInteractionModes_EscClosesProvidersManageListLocally(t *testing.T) {
	daemon := startDaemonWithOneCustomEndpoint(t, "acme")
	journey := startJourneyWithDaemonAndWorkspaceRail(t, daemon.BaseURL, 160, 50, "acme")
	journey.FocusRowDown("routing")
	journey.ActivateFocusedRow()
	journey.WaitVisible("models")
	journey.FocusRow("models")
	journey.ActivateFocusedRow()
	journey.WaitVisible("backend url")
	journey.SendKey("esc")
	journey.WaitVisible("esc back")
	journey.AssertVisibleOmits("backend url")
}
