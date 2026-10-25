package connect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/clientconnect"
)

func TestReconciliationRejectsCancellationAndChangedPlannerBinding(t *testing.T) {
	target, plan := testPlan(t)
	preview := projectPlan(target, plan)
	review := preview.Review()
	for _, stage := range []string{"before", "during", "binding"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clients := &interruptedPlanning{fakeClients: fakeClients{plan: plan}}
			switch stage {
			case "before":
				cancel()
			case "during":
				clients.cancel = cancel
			case "binding":
				clients.plan.ClientID = "claude"
			}
			service, err := NewService(clients, testCredentials{})
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Reconcile(ctx, "codex", review, target)
			if err == nil || result.State != "" || result.Plan.Client.ID != "" || clients.applied {
				t.Fatal("interrupted or rebound reconciliation acknowledged state")
			}
			if stage != "binding" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation origin lost")
			}
			if stage == "before" && clients.planCalls != 0 {
				t.Fatal("canceled reconciliation inspected files")
			}
		})
	}
}

func TestReconciliationObservesWithoutRepeatingEffects(t *testing.T) {
	target, plan := testPlan(t)
	original := projectPlan(target, plan)
	review := original.Review()
	for _, state := range []string{"unchanged", "changed", "configured", "changed-path"} {
		t.Run(state, func(t *testing.T) {
			current := plan
			switch state {
			case "changed":
				current.Changes = []clientconnect.Change{{Field: "backend", Before: "other", BeforeExists: true, After: "new"}}
			case "configured":
				current.Changes = nil
			case "changed-path":
				current.Changes = nil
				current.ConfigPaths = []string{"other/config.toml"}
			}
			clients := &fakeClients{plan: current}
			service, err := NewService(clients, testCredentials{})
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Reconcile(t.Context(), "codex", review, target)
			want := state
			if state == "changed-path" {
				want = "changed"
			}
			if err != nil || result.State != want || result.Plan.Client.ID != "codex" || clients.applied {
				t.Fatal("reconciliation lost observed state or repeated effect")
			}
		})
	}
}

func TestReconciliationRejectsReboundOrIncompleteOriginalReview(t *testing.T) {
	target, plan := testPlan(t)
	original := projectPlan(target, plan)
	for _, review := range []Review{
		{ConfigPaths: original.ConfigPaths, Changes: original.Changes},
		{Revision: "connect-forged", ConfigPaths: original.ConfigPaths, Changes: original.Changes},
		{Revision: original.Revision, ConfigPaths: []string{"other/config.toml"}, Changes: original.Changes},
		{Revision: original.Revision, ConfigPaths: original.ConfigPaths},
	} {
		clients := &fakeClients{plan: plan}
		service, err := NewService(clients, testCredentials{})
		if err != nil {
			t.Fatal(err)
		}
		result, err := service.Reconcile(t.Context(), "codex", review, target)
		if err == nil || result.State != "" || result.Plan.Client.ID != "" || clients.applied {
			t.Fatal("invalid retained review reconciled or mutated")
		}
	}
}

func TestRealFileReconciliationObservesCommitAfterOwnerRestart(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("CODEX_HOME", directory)
	path := filepath.Join(directory, "config.toml")
	const initial = "model = \"old\"\nunrelated = \"keep\"\n[model_providers.swobu.http_headers]\nAuthorization=\"Bearer fixture-client-token\"\n"
	if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(clientconnect.NewService(), testCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	target, err := clientconnect.NewTarget("research", "http://127.0.0.1:7926/workspaces/research")
	if err != nil {
		t.Fatal(err)
	}
	target = target.WithModelID("coding").WithAuthentication("structural-test-principal", "")
	preview, err := service.Plan(t.Context(), "codex", target)
	if err != nil {
		t.Fatal(err)
	}
	review := preview.Review()
	before, err := service.Reconcile(t.Context(), "codex", review, target)
	if err != nil || before.State != "unchanged" {
		t.Fatal("original file not observed unchanged")
	}
	if _, err := service.Apply(t.Context(), "codex", review, target); err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewService(clientconnect.NewService(), testCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := restarted.Reconcile(t.Context(), "codex", review, target)
	if err != nil || observed.State != "configured" {
		t.Fatal("fresh owner could not reconcile committed configuration")
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(committed) {
		t.Fatal("reconciliation changed configuration")
	}
	backup, err := os.ReadFile(path + ".swobu.bak")
	if err != nil || string(backup) != initial {
		t.Fatal("reconciliation replaced original recovery backup")
	}
}
