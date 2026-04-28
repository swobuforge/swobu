package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestB082_RunOn_AllowsReselectionAfterInitialChoice(t *testing.T) {
	journey := startFirstRunJourney(t, 80, 24)
	enterFirstRunName(t, journey, "acme")
	openFirstRunRouting(t, journey)
	ensureFirstRunProviderPickerOpen(t, journey)
	journey.FocusRow("OpenRouter")
	journey.ActivateFocusedRow()
	journey.WaitVisibleAny("run on            OpenRouter", "run on           openrouter")

	journey.FocusRow("run on")
	journey.ActivateFocusedRow()
	journey.WaitVisible("OpenAI")
	journey.FocusRow("OpenAI")
	journey.ActivateFocusedRow()
	journey.WaitVisible(">   run on")
	journey.WaitVisibleAny("run on            OpenAI", "run on           openai")
}

func TestB082_FileCredential_ShowsCredentialFileBrowseRow(t *testing.T) {
	journey := startFirstRunJourney(t, 80, 24)
	enterFirstRunName(t, journey, "acme")
	openFirstRunRouting(t, journey)
	ensureFirstRunProviderPickerOpen(t, journey)
	journey.FocusRow("OpenRouter")
	journey.ActivateFocusedRow()
	journey.WaitVisibleAny("run on            OpenRouter", "run on           openrouter")

	journey.FocusRow("credentials")
	journey.ActivateFocusedRow()
	journey.WaitVisible("file")
	journey.FocusRow("file")
	journey.ActivateFocusedRow()

	journey.WaitVisible("credential file")
	journey.WaitVisible("browse ↵")
}

func TestB082_FileCredential_BrowseListsAndAppliesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".config", "swobu")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	tokenA := filepath.Join(configDir, "openrouter.token")
	tokenB := filepath.Join(configDir, "openai.token")
	if err := os.WriteFile(tokenA, []byte("a"), 0o600); err != nil {
		t.Fatalf("write %s: %v", tokenA, err)
	}
	if err := os.WriteFile(tokenB, []byte("b"), 0o600); err != nil {
		t.Fatalf("write %s: %v", tokenB, err)
	}

	journey := startFirstRunJourney(t, 80, 24)
	enterFirstRunName(t, journey, "acme")
	openFirstRunRouting(t, journey)
	ensureFirstRunProviderPickerOpen(t, journey)
	journey.FocusRow("OpenRouter")
	journey.ActivateFocusedRow()
	journey.WaitVisibleAny("run on            OpenRouter", "run on           openrouter")

	journey.FocusRow("credentials")
	journey.ActivateFocusedRow()
	journey.WaitVisible("file")
	journey.FocusRow("file")
	journey.ActivateFocusedRow()
	journey.FocusRow("credential file")
	journey.ActivateFocusedRow()

	tokenAView := filepath.Base(tokenA)
	tokenBView := filepath.Base(tokenB)
	journey.WaitVisible(tokenAView)
	journey.WaitVisible(tokenBView)
	journey.FocusRow(tokenAView)
	journey.ActivateFocusedRow()
	journey.WaitVisible("↵ edit")
	journey.WaitVisible("credential file")
	journey.WaitVisible("browse ↵")
	journey.AssertVisibleOmits("credential file   not set")
	journey.AssertVisibleOmits("path            ")
	if visible := journey.VisibleOutput(); strings.Contains(visible, "no files") {
		t.Fatalf("expected file browser rows, got:\n%s", visible)
	}
}

func TestB082_FileCredential_BrowseIsAnchoredAndKeyboardReachable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".config", "swobu")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "openrouter.token"), []byte("a"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	journey := startFirstRunJourney(t, 80, 24)
	enterFirstRunName(t, journey, "acme")
	openFirstRunRouting(t, journey)
	ensureFirstRunProviderPickerOpen(t, journey)
	journey.FocusRow("OpenRouter")
	journey.ActivateFocusedRow()
	journey.WaitVisibleAny("run on            OpenRouter", "run on           openrouter")

	journey.FocusRow("credentials")
	journey.ActivateFocusedRow()
	journey.WaitVisible("file")
	journey.FocusRow("file")
	journey.ActivateFocusedRow()

	journey.FocusRow("credential file")
	journey.ActivateFocusedRow()
	journey.WaitVisible("path")
	journey.WaitVisibleAny(">    ../", ">   ../")

	journey.SendKey("down")
	journey.WaitVisibleAny(">    openrouter.token", ">   openrouter.token")
}
