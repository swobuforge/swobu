package tui_test

import (
	"testing"
)

func TestB076_TelemetryAccounting_RendersTrafficEvidenceWithoutPayloadContent(t *testing.T) {
	journey := givenTrafficSeededJourney(t, viewportSize{cols: 160, rows: 50})
	journey.FocusRow("traffic")
	journey.ActivateFocusedRow()
	journey.WaitVisible("chat")
	journey.AssertVisibleOmits(`"content":"hi"`)
}
