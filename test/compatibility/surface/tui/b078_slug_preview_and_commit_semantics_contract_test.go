package tui_test

import "testing"

func TestB078_SlugPreviewAndCommitSemantics_EmptyCreateNameDoesNotImplicitlyCommitEndpoint(t *testing.T) {
	journey := startJourney(t, 160, 50)
	journey.AssertVisibleContains("endpoint")
	journey.AssertVisibleContains("/c/<slug>/")
	journey.AssertVisibleOmits("invalid")
	journey.WaitVisible("name")
	journey.SendKey("down")
	journey.SendKey("up")

	journey.FocusRowDown("name")
	journey.ActivateFocusedRow()
	journey.SendKey("enter")
	journey.AssertVisibleContains("choose a workspace name")
	journey.AssertVisibleOmits("invalid endpoint name")
	journey.AssertVisibleContains("not ready")
	journey.FocusRowDown("name")
	journey.ActivateFocusedRow()
	journey.TypeText("d")
	journey.SendKey("enter")
	journey.WaitVisible("/c/d/")
	journey.AssertVisibleOmits("provider is required before save")
}
