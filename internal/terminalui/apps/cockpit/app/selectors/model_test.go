package selectors

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
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

func TestCreateDraftEndpointValue_UsesFullBaseURLWithSlugPlaceholder(t *testing.T) {
	got := CreateDraftEndpointValue(state.Model{})
	if got != "http://127.0.0.1:7926/c/<slug>/" {
		t.Fatalf("CreateDraftEndpointValue()=%q want full placeholder path", got)
	}
}

func TestCreateDraftEndpointValue_UsesFullBaseURLWithDerivedSlug(t *testing.T) {
	got := CreateDraftEndpointValue(state.Model{CreateDraftName: "Test Workspace"})
	if got != "http://127.0.0.1:7926/c/test-workspace/" {
		t.Fatalf("CreateDraftEndpointValue()=%q want full derived path", got)
	}
}
