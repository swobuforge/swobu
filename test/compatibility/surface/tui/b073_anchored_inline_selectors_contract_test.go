package tui_test

import "testing"

func TestB073_AnchoredInlineSelectors_RenderUnderCausalFieldOnCanvas(t *testing.T) {
	journey := startFirstRunJourney(t, 160, 50)
	enterFirstRunName(t, journey, "acme")
	journey.FocusRow("routing")
	journey.ActivateFocusedRow()
	journey.FocusRow("run on")
	journey.ActivateFocusedRow()
	journey.WaitVisibleAny("OpenAI", "OpenRouter")
}
