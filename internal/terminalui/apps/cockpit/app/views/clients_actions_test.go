package views

import (
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/clientprofile"
)

func TestSelectedClientActions_UsesProfileActionsForOther(t *testing.T) {
	t.Parallel()

	baseURL := "http://127.0.0.1:7926/c/acme/"
	profile := clientprofile.FindByID(clientprofile.Catalog(), "other")
	if profile == nil {
		t.Fatal("other profile missing")
	}
	actions := selectedClientActions(profile, baseURL)
	if len(actions) != 2 {
		t.Fatalf("action count=%d want 2", len(actions))
	}
	if got := actions[0].RowLabel(); got != "open" {
		t.Fatalf("row label[0]=%q", got)
	}
	if got := actions[0].ActionSummary(); got != "openai + anthropic compatible" {
		t.Fatalf("summary[0]=%q", got)
	}
	if got := actions[0].ActionVerb(); got != "view" {
		t.Fatalf("verb=%q", got)
	}
	if got := actions[1].Content; got != "Base URL: http://127.0.0.1:7926/c/acme/\nModel:    swobu" {
		t.Fatalf("copy values payload=%q", got)
	}
}

func TestSelectedClientActions_NilSelectedShowsSetup(t *testing.T) {
	t.Parallel()

	actions := selectedClientActions(nil, "http://127.0.0.1:7926/c/acme/")
	if len(actions) != 1 {
		t.Fatalf("action count=%d want 1", len(actions))
	}
	if got := actions[0].RowLabel(); got != "setup" {
		t.Fatalf("setup row=%q", got)
	}
}
