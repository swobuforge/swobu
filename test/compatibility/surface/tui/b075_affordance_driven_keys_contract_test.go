package tui_test

import "testing"

func TestB075_AffordanceDrivenKeys_SpaceMutatesOnlyToggleRows(t *testing.T) {
	daemon := startDaemonWithOneCustomEndpoint(t, "acme")
	journey := startJourneyWithDaemon(t, daemon.BaseURL, 80, 24)
	journey.FocusRow("routing")
	journey.ActivateFocusedRow()
	journey.FocusRow("run on")
	journey.SendKey("space")
	journey.AssertVisibleContains("run on")
	journey.AssertVisibleContains("choose ↵")
	journey.AssertVisibleOmits("find")
}
