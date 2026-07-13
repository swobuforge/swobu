package inspector

import (
	"os"
	"testing"
)

func TestEnabled(t *testing.T) {
	os.Setenv("SWOBU_INSPECTOR", "")
	if Enabled() {
		t.Fatal("expected disabled when env is empty")
	}
	os.Setenv("SWOBU_INSPECTOR", "layout")
	if !Enabled() {
		t.Fatal("expected enabled when env is set")
	}
}

func TestCurrentMode(t *testing.T) {
	os.Setenv("SWOBU_INSPECTOR", "")
	if got := CurrentMode(); got != ModeDiagnostics {
		t.Fatalf("empty env -> %v, want diagnostics", got)
	}
	os.Setenv("SWOBU_INSPECTOR", "layout")
	if got := CurrentMode(); got != ModeLayout {
		t.Fatalf("layout env -> %v, want layout", got)
	}
	os.Setenv("SWOBU_INSPECTOR", "focus")
	if got := CurrentMode(); got != ModeFocus {
		t.Fatalf("focus env -> %v, want focus", got)
	}
}
