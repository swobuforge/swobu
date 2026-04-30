package selectors

import (
	"testing"

	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
)

func TestFooterHints_NAVUsesEventOwnedFooterVerb(t *testing.T) {
	got := FooterHints(state.Model{
		ControlPlane: &state.ControlPlaneMismatch{
			ExpectedProtocol: 7,
		},
		FooterVerb:     "run/copy",
		FooterShowTabs: false,
	})
	if got != "↑↓ move   ↵ run/copy   ? help   esc back" {
		t.Fatalf("FooterHints = %q, want event-owned run/copy hint", got)
	}
}

func TestFooterHints_NAVUsesGlobalHelpHint(t *testing.T) {
	got := FooterHints(state.Model{
		FooterVerb: "open",
	})
	if got != "↑↓ move   ↵ open   ? help   esc back" {
		t.Fatalf("FooterHints = %q, want event-owned run/copy hint", got)
	}
}

func TestFooterHints_NAVDefaultsToActWhenNoFooterVerb(t *testing.T) {
	got := FooterHints(state.Model{
		ControlPlane: &state.ControlPlaneMismatch{
			ExpectedProtocol: 7,
		},
		FooterShowTabs: false,
	})
	if got != "↑↓ move   ↵ act   ? help   esc back" {
		t.Fatalf("FooterHints = %q, want default act hint", got)
	}
}
