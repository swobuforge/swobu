package executionevidence

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	domain "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestDirectMCPSettlementOffersExactHistoryOutsideLock(t *testing.T) {
	var store *Store
	var captured []domain.CompletedMCPOperation
	store = NewStore(Config{OfferMCP: func(operation domain.CompletedMCPOperation) bool {
		projection := store.ProjectActivity(ProjectionInput{})
		if len(projection.MCPOperations) != 1 || projection.MCPOperations[0].SettledAt.IsZero() {
			t.Fatal("history offered before terminal publication")
		}
		captured = append(captured, operation)
		return false
	}})
	slug, _ := routing.ParseWorkspaceSlug("research")
	identity, err := store.BeginMCP(accessidentity.Local, slug, "docs", "write")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SettleMCP(identity, execution.Succeeded, execution.MayHaveOccurred); err == nil {
		t.Fatal("contradictory settlement accepted")
	}
	if len(captured) != 0 {
		t.Fatal("invalid settlement entered history")
	}
	if err := store.SettleMCP(identity, execution.Failed, execution.MayHaveOccurred); err != nil {
		t.Fatalf("capture rejection changed source settlement: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("capture count=%d", len(captured))
	}
	record := store.ProjectActivity(ProjectionInput{}).MCPOperations[0]
	operation := captured[0]
	if operation.ID() != record.OperationID || operation.AccessID().String() != record.AccessID || operation.Workspace().String() != record.Workspace || operation.Source() != record.Source || operation.Tool() != record.Tool || operation.StartedAt() != record.StartedAt || operation.SettledAt() != record.SettledAt || operation.Outcome() != record.Outcome || operation.Occurrence() != record.Occurrence {
		t.Fatalf("capture changed source evidence: %#v", operation)
	}
	if err := store.SettleMCP(identity, execution.Succeeded, execution.Occurred); err == nil {
		t.Fatal("duplicate settlement accepted")
	}
	if len(captured) != 1 {
		t.Fatal("duplicate history offer")
	}
}

func TestDirectMCPOperationsRetainActiveLifecycleAndScopeWithoutExchange(t *testing.T) {
	store := NewStore(Config{SettledLimit: 1})
	research, _ := routing.ParseWorkspaceSlug("research")
	other, _ := routing.ParseWorkspaceSlug("other")
	active, err := store.BeginMCP(accessidentity.Local, research, "docs", "write")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		identity, err := store.BeginMCP(accessidentity.Local, other, "tickets", "search")
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SettleMCP(identity, execution.Succeeded, execution.Occurred); err != nil {
			t.Fatal(err)
		}
	}
	projection := store.ProjectActivity(ProjectionInput{Scope: ProjectionScope{Kind: ProjectionScopeAll}})
	if len(projection.Exchanges) != 0 || len(projection.MCPOperations) != 2 {
		t.Fatalf("projection=%#v", projection)
	}
	if err := store.SettleMCP(active, execution.Failed, execution.MayHaveOccurred); err != nil {
		t.Fatal(err)
	}
	if err := store.SettleMCP(active, execution.Succeeded, execution.Occurred); err == nil {
		t.Fatal("duplicate settlement rewrote outcome")
	}
	projection = store.ProjectActivity(ProjectionInput{Scope: ProjectionScope{Kind: ProjectionScopeWorkspace, Workspace: "research"}})
	if len(projection.MCPOperations) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	record := projection.MCPOperations[0]
	if record.OperationID != active || record.Source != "docs" || record.Tool != "write" || record.Outcome != execution.Failed || record.Occurrence != execution.MayHaveOccurred || record.SettledAt.Before(record.StartedAt) {
		t.Fatalf("record=%#v", record)
	}
	projection.MCPOperations[0].Source = "modified"
	if store.mcpOperations(projection.Scope)[0].Source != "docs" {
		t.Fatal("projection mutated retained evidence")
	}
}

func TestDirectMCPSuccessRequiresObservedExecutionWithoutLosingActiveRecord(t *testing.T) {
	store := NewStore(Config{})
	workspace, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.BeginMCP(accessidentity.Local, workspace, "docs", "write")
	if err != nil {
		t.Fatal(err)
	}
	for _, occurrence := range []execution.Occurrence{execution.MayHaveOccurred, execution.DidNotOccur} {
		if err := store.SettleMCP(identity, execution.Succeeded, occurrence); err == nil {
			t.Fatalf("success accepted contradictory occurrence %q", occurrence)
		}
		projection := store.ProjectActivity(ProjectionInput{})
		if len(projection.MCPOperations) != 1 || !projection.MCPOperations[0].SettledAt.IsZero() || projection.MCPOperations[0].Outcome != "" {
			t.Fatalf("rejected settlement changed active evidence: %#v", projection)
		}
	}
	if err := store.SettleMCP(identity, execution.Succeeded, execution.Occurred); err != nil {
		t.Fatal(err)
	}
}
