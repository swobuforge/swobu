package views

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/clientprofile"
)

func TestSelectedClientActions_UsesRunOnlyProfileActions(t *testing.T) {
	t.Parallel()

	baseURL := "http://127.0.0.1:7926/c/acme/"
	profile := clientprofile.FindByID(clientprofile.Catalog(), "codex")
	if profile == nil {
		t.Fatal("codex profile missing")
	}
	actions := selectedClientActions(profile, baseURL)
	if len(actions) != 1 {
		t.Fatalf("action count=%d want 1", len(actions))
	}
	if got := actions[0].RowLabel(); got != "run" {
		t.Fatalf("row label[0]=%q", got)
	}
	if got := actions[0].ActionSummary(); got != "command" {
		t.Fatalf("summary[0]=%q", got)
	}
	if got := actions[0].ActionVerb(); got != "run" {
		t.Fatalf("verb=%q", got)
	}
	if got := actions[0].Content; !strings.Contains(got, "codex --dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("run payload=%q", got)
	}
}

func TestSelectedClientActions_NilSelectedShowsNoActions(t *testing.T) {
	t.Parallel()

	actions := selectedClientActions(nil, "http://127.0.0.1:7926/c/acme/")
	if len(actions) != 0 {
		t.Fatalf("action count=%d want 0", len(actions))
	}
}
