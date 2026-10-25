package connect

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/clientconnect"
)

func TestApplyRejectsMissingReviewRevisionBeforeEffect(t *testing.T) {
	target, plan := testPlan(t)
	clients := &fakeClients{plan: plan}
	service, err := NewService(clients, testCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Plan(t.Context(), "codex", target)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Apply(t.Context(), "codex", Review{ConfigPaths: preview.ConfigPaths, Changes: preview.Changes}, target)
	if !errors.Is(err, ErrReviewRequired) || clients.applied {
		t.Fatal("missing review revision did not deny before apply")
	}
}

func TestApplyRejectsChangedRevisionWithMatchingEvidence(t *testing.T) {
	target, plan := testPlan(t)
	clients := &fakeClients{plan: plan}
	service, err := NewService(clients, testCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Plan(t.Context(), "codex", target)
	if err != nil {
		t.Fatal(err)
	}
	review := Review{Revision: "connect-stale", ConfigPaths: preview.ConfigPaths, Changes: preview.Changes}
	current, err := service.Apply(t.Context(), "codex", review, target)
	if !errors.Is(err, ErrReviewChanged) || current.Revision != preview.Revision || clients.applied {
		t.Fatal("changed revision applied or lost fresh inspection")
	}
	review.Revision = preview.Revision
	if _, err := service.Apply(t.Context(), "codex", review, target); err != nil || !clients.applied {
		t.Fatal("exact revision and evidence rejected")
	}
}

func TestReviewRevisionBindsDestinationAndExactEvidence(t *testing.T) {
	target, err := clientconnect.NewTarget("research", "http://127.0.0.1:7926/workspaces/research")
	if err != nil {
		t.Fatal(err)
	}
	review := Review{ConfigPaths: []string{"fixture/config.toml"}, Changes: []Change{{Field: "endpoint", Before: "old", BeforeExists: true, After: target.WorkspaceURL()}}}
	revision := reviewRevision(target, "codex", review)
	if revision == "" || revision != reviewRevision(target, "codex", review) {
		t.Fatal("identical review has unstable revision")
	}
	otherAddress, err := clientconnect.NewTarget("research", "http://127.0.0.1:7927/workspaces/research")
	if err != nil {
		t.Fatal(err)
	}
	otherWorkspace, err := clientconnect.NewTarget("coding", "http://127.0.0.1:7926/workspaces/coding")
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []string{
		reviewRevision(otherAddress, "codex", review),
		reviewRevision(otherWorkspace, "codex", review),
		reviewRevision(target, "claude", review),
		reviewRevision(target, "codex", Review{ConfigPaths: []string{"other/config.toml"}, Changes: review.Changes}),
	} {
		if changed == revision {
			t.Fatal("revision omitted destination or file identity")
		}
	}
	for _, mutate := range []func(*Change){
		func(change *Change) { change.Field = "backend" },
		func(change *Change) { change.Before = "concurrent" },
		func(change *Change) { change.BeforeExists = false },
		func(change *Change) { change.After = "other" },
	} {
		change := review.Changes[0]
		mutate(&change)
		if reviewRevision(target, "codex", Review{ConfigPaths: review.ConfigPaths, Changes: []Change{change}}) == revision {
			t.Fatal("revision omitted reviewed change evidence")
		}
	}
}
