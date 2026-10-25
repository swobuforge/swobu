package connect

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/clientconnect"
)

type fakeClients struct {
	plan       clientconnect.Plan
	applied    bool
	applyPlan  clientconnect.Plan
	applyError error
}

func (f *fakeClients) Discover(context.Context, clientconnect.Target) []clientconnect.Client {
	return nil
}
func (f *fakeClients) Plan(context.Context, clientconnect.ClientID, clientconnect.Target) (clientconnect.Plan, error) {
	return f.plan, nil
}
func (f *fakeClients) Apply(_ context.Context, plan clientconnect.Plan) (clientconnect.Plan, error) {
	f.applied = true
	if f.applyPlan.ClientID == "" {
		f.applyPlan = plan
		if f.applyError == nil {
			f.applyPlan.Changes = []clientconnect.Change{}
		}
	}
	if f.applyError == nil {
		f.applyPlan.Changes = []clientconnect.Change{}
	}
	return f.applyPlan, f.applyError
}

func testPlan(t *testing.T) (clientconnect.Target, clientconnect.Plan) {
	t.Helper()
	target, err := clientconnect.NewTarget("dev", "http://127.0.0.1:7926/workspaces/dev")
	if err != nil {
		t.Fatal(err)
	}
	target = target.WithModelID("coding")
	return target, clientconnect.Plan{ClientID: "codex", ClientName: "Codex", ConfigPaths: []string{"/tmp/config.toml"}, Target: target, Changes: []clientconnect.Change{{Field: "backend", Before: "old", BeforeExists: true, After: "new"}}}
}

func TestApplyUsesOnlyExactReviewedProjection(t *testing.T) {
	target, plan := testPlan(t)
	fake := &fakeClients{plan: plan}
	service, _ := NewService(fake, testCredentials{})
	review := Review{ConfigPaths: []string{"/tmp/config.toml"}, Changes: []Change{{Field: "backend", Before: "old", BeforeExists: true, After: "new"}}}
	review.Revision = projectPlan(target, plan).Revision
	if _, err := service.Apply(context.Background(), "codex", review, target); err != nil {
		t.Fatal(err)
	}
	if !fake.applied {
		t.Fatal("reviewed plan was not applied")
	}

	tampered := []Review{
		{ConfigPaths: []string{"/etc/passwd"}, Changes: review.Changes},
		{ConfigPaths: review.ConfigPaths, Changes: []Change{{Field: "backend", Before: "forged", BeforeExists: true, After: "new"}}},
		{ConfigPaths: review.ConfigPaths, Changes: []Change{{Field: "other", Before: "old", BeforeExists: true, After: "new"}}},
	}
	for _, candidate := range tampered {
		candidate.Revision = review.Revision
		fake.applied = false
		if _, err := service.Apply(context.Background(), "codex", candidate, target); !errors.Is(err, ErrReviewChanged) {
			t.Fatalf("tampered review error = %v", err)
		}
		if fake.applied {
			t.Fatal("tampered review reached apply")
		}
	}
}

func TestApplyPreservesSecondFreshnessCheck(t *testing.T) {
	target, plan := testPlan(t)
	fake := &fakeClients{plan: plan, applyPlan: plan, applyError: clientconnect.ErrPlanChanged}
	service, _ := NewService(fake, testCredentials{})
	review := Review{ConfigPaths: plan.ConfigPaths, Changes: []Change{{Field: "backend", Before: "old", BeforeExists: true, After: "new"}}}
	review.Revision = projectPlan(target, plan).Revision
	current, err := service.Apply(context.Background(), "codex", review, target)
	if !errors.Is(err, ErrReviewChanged) || current.Client.ID != "codex" {
		t.Fatalf("apply = %#v, %v", current, err)
	}
}

func TestPlanAndVerifiedApplyCarryHintWithoutDiscoveryMetadata(t *testing.T) {
	target, plan := testPlan(t)
	plan.PostApplyHint = "Configured. Open a new terminal before running agy."
	fake := &fakeClients{plan: plan}
	service, err := NewService(fake, testCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	projected, err := service.Plan(context.Background(), "codex", target)
	if err != nil {
		t.Fatal(err)
	}
	if projected.PostApplyHint != plan.PostApplyHint || projected.Client.Name != plan.ClientName {
		t.Fatalf("plan = %#v", projected)
	}
	verified, err := service.Apply(context.Background(), "codex", Review{
		Revision:    projected.Revision,
		ConfigPaths: projected.ConfigPaths,
		Changes:     projected.Changes,
	}, target)
	if err != nil || verified.PostApplyHint != plan.PostApplyHint {
		t.Fatalf("verified = %#v, %v", verified, err)
	}
}

func TestConnectApplicationRejectsNonLoopbackTargetForEveryOperation(t *testing.T) {
	target, err := clientconnect.NewTarget("dev", "https://swobu.example/workspaces/dev")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeClients{}
	service, err := NewService(fake, testCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Prompt(target); err == nil {
		t.Fatal("remote prompt accepted")
	}
	if _, err := service.Discover(context.Background(), target); err == nil {
		t.Fatal("remote discovery accepted")
	}
	if _, err := service.Plan(context.Background(), "codex", target); err == nil {
		t.Fatal("remote plan accepted")
	}
	if _, err := service.Apply(context.Background(), "codex", Review{}, target); err == nil {
		t.Fatal("remote apply accepted")
	}
	if fake.applied {
		t.Fatal("remote target reached client mutation")
	}
}

func TestConnectApplicationProjectsWorkspaceFromTarget(t *testing.T) {
	target, plan := testPlan(t)
	service, err := NewService(&fakeClients{plan: plan}, testCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := service.Prompt(target)
	if err != nil || prompt.Workspace != target.WorkspaceSlug() {
		t.Fatalf("prompt = %#v, %v", prompt, err)
	}
	projected, err := service.Plan(context.Background(), "codex", target)
	if err != nil || projected.Workspace != target.WorkspaceSlug() {
		t.Fatalf("plan = %#v, %v", projected, err)
	}
}

func TestConnectReviewIdentityBindsRequestedModelID(t *testing.T) {
	target, plan := testPlan(t)
	first := projectPlan(target, plan)
	other := target.WithModelID("claude-sonnet-4")
	second := projectPlan(other, plan)
	if first.ModelID != "coding" || second.ModelID != "claude-sonnet-4" || first.Revision == second.Revision {
		t.Fatal("changing client-requested model did not change Connect review identity")
	}
}
