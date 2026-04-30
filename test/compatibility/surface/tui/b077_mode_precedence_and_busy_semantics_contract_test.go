package tui_test

import "testing"

func TestB077_ModePrecedence_EditModeConsumesTextInputBeforeNavTraversal(t *testing.T) {
	journey := startJourney(t, 160, 50)
	journey.WaitVisible("name")
	journey.SendKey("down")
	journey.ActivateFocusedRow()
	journey.SendKey("_")
	journey.WaitVisible("__")
}
