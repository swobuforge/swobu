package testharness

import (
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
)

// KeyEvent builds one keyboard interaction event.
func KeyEvent(key interaction.Key) interaction.Event {
	return interaction.Event{Kind: interaction.EventKey, Key: key}
}

// FocusRowContaining moves focus down until a focused line contains pattern.
func FocusRowContaining(t *testing.T, render func() string, stepDown func(), pattern string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		out := render()
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, ">") && strings.Contains(line, pattern) {
				return
			}
		}
		before := out
		stepDown()
		waitForRenderChange(render, before)
	}
	t.Fatalf("could not focus row containing %q; render=%q", pattern, render())
}

// FocusChooserOptionContaining moves focus in a chooser until option line matches pattern.
func FocusChooserOptionContaining(t *testing.T, render func() string, stepDown func(), pattern string) {
	t.Helper()
	for i := 0; i < 30; i++ {
		out := render()
		for _, line := range strings.Split(out, "\n") {
			normalized := strings.ToLower(strings.TrimSpace(line)) // swobu:io-string source=boundary
			if !strings.Contains(line, ">") || !strings.Contains(line, pattern) {
				continue
			}
			if strings.Contains(normalized, "provider") || strings.Contains(normalized, "credential") {
				continue
			}
			return
		}
		before := out
		stepDown()
		waitForRenderChange(render, before)
	}
	t.Fatalf("could not focus chooser option containing %q; render=%q", pattern, render())
}

// FocusedLineContains reports whether the currently focused line includes token.
func FocusedLineContains(out string, token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))                // swobu:io-string source=boundary
	for _, line := range strings.Split(strings.ToLower(out), "\n") { // swobu:io-string source=boundary
		if !strings.Contains(line, token) {
			continue
		}
		if strings.Contains(line, ">") || strings.Contains(line, "›") {
			return true
		}
	}
	return false
}

func waitForRenderChange(render func() string, before string) {
	if render == nil {
		return
	}
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		if render() != before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
