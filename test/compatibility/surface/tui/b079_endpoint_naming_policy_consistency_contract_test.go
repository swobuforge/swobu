package tui_test

import "testing"

func TestB079_EndpointNamingPolicyConsistency_InvalidCharactersAreNotAutoNormalized(t *testing.T) {
	journey := startJourney(t, 160, 50)
	journey.WaitVisible("name")
	journey.FocusRowDown("name")
	journey.ActivateFocusedRow()

	journey.SendKey("_")
	journey.WaitVisible("__")
	visible := journey.VisibleOutput()
	journey.AssertVisibleContains("__")
	journey.AssertVisibleOmits("-")
	if visible == "" {
		t.Fatalf("visible output unexpectedly empty")
	}
}
