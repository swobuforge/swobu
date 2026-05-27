package testharness

import (
	"strings"
	"testing"

	baseharness "github.com/swobuforge/swobu/internal/terminalui/testharness"
)

// OpenAddModelAndChooseProvider navigates cockpit interactions to open add-model
// flow and select one provider option.
func OpenAddModelAndChooseProvider(t *testing.T, render func() string, keyDown func(), keyEnter func(), providerName string) {
	t.Helper()
	baseharness.FocusRowContaining(t, render, keyDown, "routing")
	keyEnter()
	baseharness.FocusRowContaining(t, render, keyDown, "models")
	keyEnter()
	baseharness.FocusRowContaining(t, render, keyDown, "add model")
	keyEnter()
	baseharness.FocusRowContaining(t, render, keyDown, "provider")
	keyEnter()
	baseharness.FocusChooserOptionContaining(t, render, keyDown, providerName)
	keyEnter()
}

// ChooseAddModelAuthOption selects one credential strategy option in add-model flow.
func ChooseAddModelAuthOption(t *testing.T, render func() string, keyDown func(), keyEnter func(), option string) {
	t.Helper()
	baseharness.FocusRowContaining(t, render, keyDown, "credential")
	keyEnter()
	baseharness.FocusChooserOptionContaining(t, render, keyDown, option)
	keyEnter()
}

// SelectClientFromChooser opens clients chooser and selects one client label.
func SelectClientFromChooser(t *testing.T, render func() string, keyDown func(), keyEnter func(), label string) {
	t.Helper()
	baseharness.FocusRowContaining(t, render, keyDown, "clients")
	keyEnter()
	baseharness.FocusRowContaining(t, render, keyDown, "client            ")
	keyEnter()
	baseharness.FocusRowContaining(t, render, keyDown, label)
	keyEnter()
}

// SelectAddModelFileCredential selects "file" credential strategy and verifies
// the follow-up file row appears.
func SelectAddModelFileCredential(t *testing.T, render func() string, keyDown func(), keyEnter func()) {
	t.Helper()
	baseharness.FocusRowContaining(t, render, keyDown, "add model")
	keyDown()
	keyDown()
	keyEnter()
	baseharness.FocusChooserOptionContaining(t, render, keyDown, "file")
	keyEnter()
	if strings.Contains(render(), "credential file") {
		return
	}
	t.Fatalf("unable to select file credential option in add-model flow; render=%q", render())
}

// EnsureSectionOpenFromAnyFocusState escapes transient modes, focuses section,
// and verifies expanded state.
func EnsureSectionOpenFromAnyFocusState(t *testing.T, render func() string, keyDown func(), keyEnter func(), keyEsc func(), sectionToken string, expandedToken string) {
	t.Helper()
	for i := 0; i < 3; i++ {
		keyEsc()
	}
	for i := 0; i < 120; i++ {
		out := render()
		if baseharness.FocusedLineContains(out, sectionToken) {
			break
		}
		keyDown()
		if i == 119 {
			t.Fatalf("%s row not reachable; render=%q", sectionToken, out)
		}
	}
	keyEnter()
	out := render()
	if !strings.Contains(out, expandedToken) {
		t.Fatalf("section did not expand (%s); render=%q", expandedToken, out)
	}
}
