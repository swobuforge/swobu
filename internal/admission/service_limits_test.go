package admission

import (
	"errors"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestSharedWorkspaceBudgetAndConcurrency(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("engineering")
	policy := routing.ServiceLimits{Models: routing.ServiceQuota{RequestsPerMinute: 2, Concurrent: 1}, MCP: routing.ServiceQuota{RequestsPerMinute: 1}}
	life := "one"
	limiter := New(func(routing.WorkspaceSlug) (string, routing.ServiceLimits, error) { return life, policy, nil })
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	release, _, err := limiter.Admit(slug, routing.ServiceModels)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := limiter.Admit(slug, routing.ServiceModels); !errors.Is(err, ErrExhausted) {
		t.Fatalf("concurrency gate=%v", err)
	}
	release()
	release()
	release2, _, err := limiter.Admit(slug, routing.ServiceModels)
	if err != nil {
		t.Fatal(err)
	}
	release2()
	if _, retry, err := limiter.Admit(slug, routing.ServiceModels); !errors.Is(err, ErrExhausted) || retry < time.Second {
		t.Fatalf("rate gate=%v %v", err, retry)
	}
	// Independent Service budget.
	if _, _, err := limiter.Admit(slug, routing.ServiceMCP); err != nil {
		t.Fatal(err)
	}
	if _, _, err := limiter.Admit(slug, routing.ServiceMCP); !errors.Is(err, ErrExhausted) {
		t.Fatal("MCP budget was not charged")
	}
	now = now.Add(time.Minute)
	if _, _, err := limiter.Admit(slug, routing.ServiceModels); err != nil {
		t.Fatal(err)
	}
	life = "two" // Recreating a Workspace starts a distinct lifetime.
	if _, _, err := limiter.Admit(slug, routing.ServiceModels); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceLimitEditDoesNotRefreshConsumedBudget(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("engineering")
	policy := routing.ServiceLimits{Search: routing.ServiceQuota{RequestsPerMinute: 1}}
	limiter := New(func(routing.WorkspaceSlug) (string, routing.ServiceLimits, error) { return "same", policy, nil })
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	if _, _, err := limiter.Admit(slug, routing.ServiceSearch); err != nil {
		t.Fatal(err)
	}
	policy.Search.RequestsPerMinute = 2
	if _, _, err := limiter.Admit(slug, routing.ServiceSearch); !errors.Is(err, ErrExhausted) {
		t.Fatalf("policy edit refreshed consumed budget: %v", err)
	}
	now = now.Add(30 * time.Second)
	if _, _, err := limiter.Admit(slug, routing.ServiceSearch); err != nil {
		t.Fatal(err)
	}
}
func TestWorkspaceLookupFailureDoesNotAdmit(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("engineering")
	limiter := New(func(routing.WorkspaceSlug) (string, routing.ServiceLimits, error) {
		return "", routing.ServiceLimits{}, errors.New("gone")
	})
	if _, _, err := limiter.Admit(slug, routing.ServiceModels); err == nil {
		t.Fatal("deleted Workspace admitted")
	}
}

func TestEnablingModelConcurrencyCountsAlreadyAdmittedOperations(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("engineering")
	policy := routing.ServiceLimits{}
	limiter := New(func(routing.WorkspaceSlug) (string, routing.ServiceLimits, error) { return "same", policy, nil })
	release, _, err := limiter.Admit(slug, routing.ServiceModels)
	if err != nil {
		t.Fatal(err)
	}
	policy.Models.Concurrent = 1
	unexpected, _, err := limiter.Admit(slug, routing.ServiceModels)
	if !errors.Is(err, ErrExhausted) {
		unexpected()
		t.Fatalf("enabling concurrency ignored an in-flight operation: %v", err)
	}
	release()
	release()
	final, _, err := limiter.Admit(slug, routing.ServiceModels)
	if err != nil {
		t.Fatalf("old operation did not return its slot: %v", err)
	}
	final()
}

func TestEnablingModelRateDoesNotRefillFromUnlimitedInterval(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("engineering")
	policy := routing.ServiceLimits{}
	limiter := New(func(routing.WorkspaceSlug) (string, routing.ServiceLimits, error) { return "same", policy, nil })
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	release, _, err := limiter.Admit(slug, routing.ServiceModels)
	if err != nil {
		t.Fatal(err)
	}
	release()
	now = now.Add(10 * time.Minute)
	policy.Models.RequestsPerMinute = 1
	release, _, err = limiter.Admit(slug, routing.ServiceModels)
	if err != nil {
		t.Fatal(err)
	}
	release()
	unexpected, _, err := limiter.Admit(slug, routing.ServiceModels)
	if !errors.Is(err, ErrExhausted) {
		unexpected()
		t.Fatalf("unlimited interval minted a second rate token: %v", err)
	}
}
