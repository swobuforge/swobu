package tui_test

import "testing"

func TestB081_CustomProviderSemantics_ProviderCopyStatesOpenAICompatibleURLFormat(t *testing.T) {
	daemon := startDaemonWithOneCustomEndpoint(t, "acme")
	journey := startJourneyWithDaemonAndWorkspaceRail(t, daemon.BaseURL, 160, 50, "[› acme]")
	journey.FocusRow("routing")
	journey.ActivateFocusedRow()
	journey.WaitVisible("models")
	journey.FocusRow("models")
	journey.ActivateFocusedRow()
	journey.WaitVisible("backend url")
	journey.AssertVisibleContains("/v1")
}
