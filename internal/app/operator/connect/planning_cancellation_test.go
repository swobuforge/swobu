package connect

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/swobuforge/swobu/internal/clientconnect"
)

type interruptedPlanning struct {
	fakeClients
	planCalls int
	cancel    context.CancelFunc
}

type interruptedApply struct {
	fakeClients
	cancel     context.CancelFunc
	applyError error
}

func (clients *interruptedApply) Apply(context.Context, clientconnect.Plan) (clientconnect.Plan, error) {
	clients.applied = true
	clients.cancel()
	result := clients.plan
	if clients.applyError == nil {
		result.Changes = []clientconnect.Change{}
	}
	return result, clients.applyError
}

func (clients *interruptedPlanning) Plan(context.Context, clientconnect.ClientID, clientconnect.Target) (clientconnect.Plan, error) {
	clients.planCalls++
	if clients.cancel != nil {
		clients.cancel()
	}
	return clients.plan, nil
}

func TestConnectCancellationBeforePlanningCannotAcknowledgeOrApply(t *testing.T) {
	for _, operation := range []string{"preview", "apply"} {
		for _, stage := range []string{"before", "during", "healthy"} {
			t.Run(operation+"/"+stage, func(t *testing.T) {
				target, plan := testPlan(t)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				clients := &interruptedPlanning{fakeClients: fakeClients{plan: plan}}
				if stage == "before" {
					cancel()
				}
				if stage == "during" {
					clients.cancel = cancel
				}
				service, err := NewService(clients, testCredentials{})
				if err != nil {
					t.Fatal(err)
				}
				var result Plan
				if operation == "preview" {
					result, err = service.Plan(ctx, "codex", target)
				} else {
					review := Review{ConfigPaths: []string{"/tmp/config.toml"}, Changes: []Change{{Field: "backend", Before: "old", BeforeExists: true, After: "new"}}}
					review.Revision = projectPlan(target, plan).Revision
					result, err = service.Apply(ctx, "codex", review, target)
				}
				if stage == "healthy" {
					if err != nil || result.Client.ID != "codex" || clients.planCalls != 1 || clients.applied != (operation == "apply") {
						t.Fatal("healthy operation lost owner behavior")
					}
					return
				}
				if !errors.Is(err, context.Canceled) || result.Client.ID != "" || clients.applied {
					t.Fatal("canceled planning acknowledged or entered apply")
				}
				if stage == "before" && clients.planCalls != 0 {
					t.Fatal("already canceled context reached planner")
				}
				if stage == "during" && clients.planCalls != 1 {
					t.Fatal("interrupted plan retried")
				}
			})
		}
	}
}

func TestConnectLateCancellationPreservesApplyOwnerResult(t *testing.T) {
	uncertain := errors.New("external acknowledgement unavailable")
	for _, outcome := range []error{nil, uncertain} {
		t.Run(fmt.Sprint(outcome), func(t *testing.T) {
			target, plan := testPlan(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clients := &interruptedApply{fakeClients: fakeClients{plan: plan}, cancel: cancel, applyError: outcome}
			service, err := NewService(clients, testCredentials{})
			if err != nil {
				t.Fatal(err)
			}
			review := Review{ConfigPaths: []string{"/tmp/config.toml"}, Changes: []Change{{Field: "backend", Before: "old", BeforeExists: true, After: "new"}}}
			review.Revision = projectPlan(target, plan).Revision
			result, err := service.Apply(ctx, "codex", review, target)
			if !clients.applied || result.Client.ID != "codex" || result.Workspace != target.WorkspaceSlug() || !errors.Is(err, outcome) || ctx.Err() != context.Canceled {
				t.Fatal("late cancellation erased owner result or manufactured rollback")
			}
		})
	}
}
