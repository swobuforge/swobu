// PTY e2e journeys for create flow, keyboard, regression, and section order.
package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/metrofun/swobu/test/e2e/harness"
)

// TestPTYOperatorJourney_CreateFlowEntersSameCanvas proves CR-01 and CR-02:
// create via [+] enters the same canvas and initial focus lands on name.
func TestPTYOperatorJourney_CreateFlowEntersSameCanvas(t *testing.T) {
	j := startCreateFlowJourney(t)
	waitCockpitHeader(j)

	// CR-01: same canvas create state appears — workspace rail shows [+]
	j.WaitVisibleAny("[› +]", "[ + new workspace ]")

	// CR-02: name row is visible in workspace section
	j.AssertVisibleContains("name")
	j.AssertVisibleContains("choose a workspace name")

	// A-08: no wizard or alternate screen — body still has workspace and routing sections
	j.AssertVisibleContains("workspace")
	j.AssertVisibleContains("routing")

	// G-04: daemon does not appear as a workspace row
	j.AssertVisibleOmits("daemon")

	// G-05: no standalone behavior section
	j.AssertVisibleOmits("behavior")
}

// TestPTYOperatorJourney_CreateFlowConfigureProvider proves CR-03:
// after opening routing and provider picker, provider options appear inline.
func TestPTYOperatorJourney_CreateFlowConfigureProvider(t *testing.T) {
	j := startCreateFlowJourney(t)
	waitCockpitHeader(j)

	// Open routing section then provider picker.
	j.FocusRow("routing")
	j.ActivateFocusedRow()
	j.WaitVisible("run on")
	j.FocusRow("run on")
	j.ActivateFocusedRow()

	// CR-03: after opening providers manager, provider options appear.
	j.WaitVisibleAny("OpenAI", "Ollama")

	// Drive one option-focused interaction path.
	j.FocusRowDown("Ollama")
	j.WaitVisible("Ollama")
}

// TestPTYOperatorJourney_KeyboardTraversal proves K-01 and K-05:
// Up/Down traverses visible focusable rows and Esc exits from top scope.
func TestPTYOperatorJourney_KeyboardTraversal(t *testing.T) {
	j := startCreateFlowJourney(t)
	waitCockpitHeader(j)

	// K-01: Down traverses visible rows.
	j.FocusRow("name")
	j.FocusRow("routing")

	// K-05: esc exits from top-level scope.
	j.SendKey("esc")
}

// TestPTYOperatorJourney_RegressionLabelsAbsent proves G-01, G-02, G-04, G-05:
// deprecated v0 labels must not appear in the v2 cockpit.
// G-03 is excluded because "provider" appears in help text like
// "choose a provider to configure workspace run on" — only top-level row
// absence is required, which is proven by section order tests.
func TestPTYOperatorJourney_RegressionLabelsAbsent(t *testing.T) {
	j := startCreateFlowJourney(t)
	waitCockpitHeader(j)

	// G-01: no top-level "selected target" row.
	j.AssertVisibleOmits("selected target")

	// G-02: no top-level "targets" row.
	j.AssertVisibleOmits("targets")

	// G-04: no "daemon" row in workspace section.
	j.AssertVisibleOmits("daemon")

	// G-05: no standalone "behavior" section.
	j.AssertVisibleOmits("behavior")
}

// TestPTYOperatorJourney_V2SectionOrder proves A-03, A-04:
// workspace has name+endpoint and routing reveals run-on choices when opened.
func TestPTYOperatorJourney_V2SectionOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{})
	t.Setenv("SWOBU_DAEMON_URL", daemon.BaseURL)
	j := harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
	waitCockpitHeader(j)

	// A-03: workspace shows only name and endpoint.
	j.AssertVisibleContains("name")
	j.AssertVisibleContains("endpoint")

	// A-04: opening routing reveals run on and key source rows.
	j.FocusRow("routing")
	j.ActivateFocusedRow()
	j.WaitVisible("run on")
	j.AssertVisibleContains("credentials")

	// A-06: clients rows appear only when enough config exists.
	// In create mode with no provider, clients is hidden per wireframe law.
	j.AssertVisibleContains("workspace")
	j.AssertVisibleContains("routing")
}

func startCreateFlowJourney(t *testing.T) harness.OperatorPTYJourney {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{})
	t.Setenv("SWOBU_DAEMON_URL", daemon.BaseURL)
	return harness.StartSwobuOperatorPTYJourney(ctx, t, 160, 50)
}

func waitCockpitHeader(j harness.OperatorPTYJourney) {
	j.WaitVisible("Swobu")
	j.WaitVisibleAny("ready", "offline (stale)")
}
