package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/metrofun/swobu/test/e2e/harness"
)

// TestFSeries_F01_FirstLaunch asserts the entry point for first-run
// progressive onboarding when no workspaces exist.
func TestFSeries_F01_FirstLaunch(t *testing.T) {
	journey := startFirstRunJourney(t, 80, 24)
	journey.WaitVisible("[ + new workspace ]")
	journey.WaitVisible("workspace")
	journey.WaitVisible("routing")
	journey.WaitVisible("name")
	journey.WaitVisible("create")
	journey.FocusRow("name")

	visible := journey.VisibleOutput()
	assertWireframeEqualsFixture(t, visible, "F-01_first-launch.txt")
}

// TestFSeries_F02_NameEntered asserts the state after the operator commits
// a workspace name on first run.
func TestFSeries_F02_NameEntered(t *testing.T) {
	journey := startFirstRunJourney(t, 80, 24)

	// Focus the name row and edit it.
	journey.FocusRow("name")
	journey.ActivateFocusedRow()
	journey.WaitVisible("↵ save")
	journey.TypeText("acme")
	journey.SendKey("enter")

	journey.WaitVisible("acme")
	journey.FocusRow("routing")

	visible := journey.VisibleOutput()
	assertWireframeEqualsFixture(t, visible, "F-02_name-entered.txt")
}

// TestFSeries_F03_ProviderPickerOpen asserts the state after the operator
// opens the provider chooser from the first-run screen.
func TestFSeries_F03_ProviderPickerOpen(t *testing.T) {
	journey := startFirstRunJourney(t, 80, 24)

	enterFirstRunName(t, journey, "acme")

	// Open the provider picker.
	openFirstRunRouting(t, journey)
	ensureFirstRunProviderPickerOpen(t, journey)

	visible := journey.VisibleOutput()
	assertWireframeEqualsFixture(t, visible, "F-03_provider-picker-open.txt")
}

// TestFSeries_F04_ProviderChosen asserts the state after a provider is
// selected from the first-run picker.
func TestFSeries_F04_ProviderChosen(t *testing.T) {
	journey := startFirstRunJourney(t, 80, 24)

	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)

	visible := journey.VisibleOutput()
	assertWireframeEqualsFixture(t, visible, "F-04_provider-chosen.txt")
}

// TestFSeries_F05_ModelPickerOpen asserts first-run model picker opens with
// a focused first result and visible query row.
func TestFSeries_F05_ModelPickerOpen(t *testing.T) {
	journey := startFirstRunJourney(t, 80, 24)

	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)
	openFirstRunModelPicker(t, journey)

	visible := journey.VisibleOutput()
	assertWireframeEqualsFixture(t, visible, "F-05_model-picker-open.txt")
}

// TestFSeries_F06_ReadyToCreate asserts first-run remains blocked when the
// operator switches to file credentials but has not chosen a file.
func TestFSeries_F06_ReadyToCreate(t *testing.T) {
	journey := startFirstRunJourney(t, 80, 24)

	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)
	switchFirstRunCredentialSourceToFile(t, journey)
	journey.WaitVisible("credential file")
	journey.WaitVisible("not set")
	journey.WaitVisible("create")
	journey.WaitVisible("not ready")
	journey.FocusRow("credential file")

	visible := journey.VisibleOutput()
	assertWireframeEqualsFixture(t, visible, "F-06_ready-to-create.txt")
}

// TestFSeries_F07_Creating asserts file browser opens and first-run does not
// enter a saving state.
func TestFSeries_F07_Creating(t *testing.T) {
	journey := startFirstRunJourney(t, 80, 24)

	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)
	switchFirstRunCredentialSourceToFile(t, journey)

	journey.FocusRow("credential file")
	journey.ActivateFocusedRow()
	journey.WaitVisible("path")
	journey.WaitVisibleAny(">    ../", ">   ../")
	journey.WaitVisible("not ready")
	journey.AssertVisibleOmits("saving")

	visible := journey.VisibleOutput()
	assertWireframeEqualsFixture(t, visible, "F-07_creating.txt")
}

// TestFSeries_F08_SuccessTransition asserts choosing a credential file updates
// routing state but onboarding does not transition without model selection.
func TestFSeries_F08_SuccessTransition(t *testing.T) {
	home := "/tmp/swobu-f08-home"
	_ = os.RemoveAll(home)
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".config", "swobu")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	tokenPath := filepath.Join(configDir, "openrouter.token")
	if err := os.WriteFile(tokenPath, []byte("token"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	journey := startFirstRunJourney(t, 80, 24)

	enterFirstRunName(t, journey, "acme")
	selectFirstRunProvider(t, journey)
	switchFirstRunCredentialSourceToFile(t, journey)

	journey.FocusRow("credential file")
	journey.ActivateFocusedRow()
	journey.WaitVisible("path")
	journey.WaitVisible("openrou")
	journey.FocusRowDown("openrou")
	journey.ActivateFocusedRow()
	journey.WaitVisible("[ + new workspace ]")
	journey.AssertVisibleOmits("[› acme] [ + ]")
	journey.WaitVisible("credential file")
	journey.WaitVisible("openrou")
	journey.FocusRow("credential file")

	visible := journey.VisibleOutput()
	assertWireframeEqualsFixture(t, visible, "F-08_success-transition.txt")
}

func enterFirstRunName(t *testing.T, journey harness.OperatorPTYJourney, name string) {
	t.Helper()
	journey.WaitVisible("choose a workspace name")
	for i := 0; i < 3 && !firstRunNameEditorOpen(journey.VisibleOutput()); i++ {
		journey.SendKey("up")
		time.Sleep(20 * time.Millisecond)
	}
	for i := 0; i < 3 && !firstRunNameEditorOpen(journey.VisibleOutput()); i++ {
		journey.SendKey("down")
		journey.ActivateFocusedRow()
		time.Sleep(60 * time.Millisecond)
	}
	journey.WaitVisibleAny("↵ save", "save ↵")
	journey.TypeText(name)
	journey.SendKey("enter")
	journey.WaitVisible(name)
}

func firstRunNameEditorOpen(visible string) bool {
	return strings.Contains(visible, "↵ save") || strings.Contains(visible, "save ↵")
}

func openFirstRunRouting(t *testing.T, journey harness.OperatorPTYJourney) {
	t.Helper()
	journey.FocusRow("routing")
	visible := strings.ToLower(journey.VisibleOutput())
	if !strings.Contains(visible, "run on") {
		journey.ActivateFocusedRow()
	}
	journey.WaitVisible("run on")
	journey.FocusRow("run on")
}

func selectFirstRunProvider(t *testing.T, journey harness.OperatorPTYJourney) {
	t.Helper()
	openFirstRunRouting(t, journey)
	ensureFirstRunProviderPickerOpen(t, journey)
	journey.FocusRow("OpenRouter")
	journey.ActivateFocusedRow()
	journey.WaitVisibleAny("run on            OpenRouter", "run on           openrouter")
	journey.WaitVisibleAny("choose a key source", "credentials")
}

func ensureFirstRunProviderPickerOpen(t *testing.T, journey harness.OperatorPTYJourney) {
	t.Helper()
	journey.FocusRow("run on")
	visible := strings.ToLower(journey.VisibleOutput())
	if !strings.Contains(visible, "openai") {
		journey.ActivateFocusedRow()
	}
	journey.WaitVisibleAny("OpenAI", "openai", "- openai")
}

func openFirstRunModelPicker(t *testing.T, journey harness.OperatorPTYJourney) bool {
	t.Helper()
	journey.FocusRow("credentials")
	journey.ActivateFocusedRow()
	journey.WaitVisible("env")
	journey.FocusRow("env")
	journey.ActivateFocusedRow()
	journey.WaitVisible("model")
	journey.FocusRow("model")
	journey.ActivateFocusedRow()
	journey.WaitVisibleAny("find", "unavailable")
	if strings.Contains(strings.ToLower(journey.VisibleOutput()), "unavailable") {
		return false
	}
	journey.TypeText("gpt-4.1")
	journey.WaitVisible("gpt-4.1")
	return true
}

func switchFirstRunCredentialSourceToFile(t *testing.T, journey harness.OperatorPTYJourney) {
	t.Helper()
	journey.FocusRow("credentials")
	journey.ActivateFocusedRow()
	journey.WaitVisible("file")
	journey.FocusRow("file")
	journey.ActivateFocusedRow()
}

func chooseFirstRunKeyAndModel(t *testing.T, journey harness.OperatorPTYJourney) {
	t.Helper()
	for attempt := 0; attempt < 3; attempt++ {
		if openFirstRunModelPicker(t, journey) {
			journey.ActivateFocusedRow()
			journey.WaitVisibleAny("gpt-4.1", "openai/gpt-4.1")
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Skip("model picker unavailable; skipping first-run model selection")
}

func startFirstRunJourney(t *testing.T, cols int, rows int) harness.OperatorPTYJourney {
	t.Helper()
	t.Setenv("OPENROUTER_API_KEY", "test-token")
	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{})
	return startJourneyWithDaemon(t, daemon.BaseURL, cols, rows)
}
