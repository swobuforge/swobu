package connect

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/swobuforge/swobu/internal/clientconnect"
)

func TestInvalidPostEffectBindingNeverPublishesCompletedReceipt(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	attempts, err := OpenAttempts(directory)
	if err != nil {
		t.Fatal(err)
	}
	target, plan := testPlan(t)
	verified := plan
	verified.Changes = nil
	verified.Target = clientconnect.Target{}
	clients := &fakeClients{plan: plan, applyPlan: verified}
	service, err := NewService(clients, testCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	preview := projectPlan(target, plan)
	review := preview.Review()
	result, err := service.ApplyAttempt(t.Context(), attempts, "invalid-readback-original-key", "codex", review, target, func() error { return nil })
	if err == nil || result.Client.ID != "" || result.Replayed || !clients.applied {
		t.Fatal("invalid readback acknowledged apply attempt")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		t.Fatal("invalid readback published completion or lost attempt marker")
	}
	clients.applied = false
	if _, err := service.ApplyAttempt(t.Context(), attempts, "invalid-readback-original-key", "codex", review, target, func() error { return nil }); !errors.Is(err, ErrAttemptIndeterminate) || clients.applied {
		t.Fatal("invalid readback retry repeated uncertain effect")
	}
}

func TestConnectRejectsPostEffectReadbackBindingMismatch(t *testing.T) {
	for _, mismatch := range []string{"client", "target", "missing"} {
		for _, outcome := range []error{nil, errors.New("external effect acknowledgement unavailable")} {
			t.Run(mismatch+"/"+fmt.Sprint(outcome), func(t *testing.T) {
				target, plan := testPlan(t)
				verified := plan
				verified.Changes = nil
				switch mismatch {
				case "client":
					verified.ClientID = "claude"
				case "target":
					var err error
					verified.Target, err = clientconnect.NewTarget("other", "http://127.0.0.1:7926/workspaces/other")
					if err != nil {
						t.Fatal(err)
					}
				case "missing":
					verified.Target = clientconnect.Target{}
				}
				clients := &fakeClients{plan: plan, applyPlan: verified, applyError: outcome}
				service, err := NewService(clients, testCredentials{})
				if err != nil {
					t.Fatal(err)
				}
				preview, err := service.Plan(t.Context(), "codex", target)
				if err != nil {
					t.Fatal(err)
				}
				result, err := service.Apply(t.Context(), "codex", preview.Review(), target)
				if err == nil || result.Client.ID != "" || result.AlreadyConfigured || !clients.applied {
					t.Fatal("post-effect invalid binding acknowledged requested configuration")
				}
				if outcome != nil && !errors.Is(err, outcome) {
					t.Fatal("post-effect owner uncertainty origin erased")
				}
			})
		}
	}
}

func TestConnectRejectsPlannerBindingMismatchBeforeProjectionOrApply(t *testing.T) {
	for _, mismatch := range []string{"client", "workspace", "address", "missing-target"} {
		for _, operation := range []string{"preview", "apply"} {
			t.Run(mismatch+"/"+operation, func(t *testing.T) {
				target, plan := testPlan(t)
				switch mismatch {
				case "client":
					plan.ClientID = "claude"
				case "workspace":
					var err error
					plan.Target, err = clientconnect.NewTarget("other", "http://127.0.0.1:7926/workspaces/other")
					if err != nil {
						t.Fatal(err)
					}
				case "address":
					var err error
					plan.Target, err = clientconnect.NewTarget("dev", "http://127.0.0.1:9999/workspaces/dev")
					if err != nil {
						t.Fatal(err)
					}
				case "missing-target":
					plan.Target = clientconnect.Target{}
				}
				clients := &fakeClients{plan: plan}
				service, err := NewService(clients, testCredentials{})
				if err != nil {
					t.Fatal(err)
				}
				var result Plan
				if operation == "preview" {
					result, err = service.Plan(context.Background(), "codex", target)
				} else {
					result, err = service.Apply(context.Background(), "codex", Review{ConfigPaths: plan.ConfigPaths, Changes: []Change{{Field: "backend", Before: "old", BeforeExists: true, After: "new"}}}, target)
				}
				if err == nil || result.Client.ID != "" || clients.applied {
					t.Fatal("mismatched planner binding acknowledged or applied")
				}
			})
		}
	}
}

func TestConnectCanonicalBindingPreservesReviewAndApply(t *testing.T) {
	_, plan := testPlan(t)
	target, err := clientconnect.NewTarget("research", "http://127.0.0.1:7926/workspaces/research")
	if err != nil {
		t.Fatal(err)
	}
	target = target.WithModelID("coding")
	plan.Target = target
	clients := &fakeClients{plan: plan}
	service, err := NewService(clients, testCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Plan(t.Context(), "codex", target)
	if err != nil || preview.Workspace != "research" || preview.Client.ID != "codex" || clients.applied {
		t.Fatal("canonical preview lost binding or mutated")
	}
	result, err := service.Apply(t.Context(), "codex", preview.Review(), target)
	if err != nil || result.Workspace != "research" || result.Client.ID != "codex" || !clients.applied {
		t.Fatal("canonical reviewed apply lost binding")
	}
}
