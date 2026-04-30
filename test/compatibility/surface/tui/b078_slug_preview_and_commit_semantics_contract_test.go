package tui_test

import "testing"

func TestB078_SlugPreviewAndCommitSemantics_EmptyCreateNameDoesNotImplicitlyCommitEndpoint(t *testing.T) {
	journey := startJourney(t, 160, 50)
	journey.AssertVisibleContains("endpoint")
	journey.AssertVisibleContains("/c/<slug>/")
	journey.AssertVisibleOmits("invalid")
	journey.AssertVisibleContains("not ready")
	journey.SendKey("down")
	journey.SendKey("up")
	journey.AssertVisibleContains("/c/<slug>/")
	journey.AssertVisibleOmits("invalid endpoint name")
}
