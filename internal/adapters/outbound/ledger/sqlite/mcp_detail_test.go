package sqlite

import (
	"fmt"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestExactMCPDetailBeyondHeadSurvivesRestart(t *testing.T) {
	cfg := testConfig(t)
	db, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	workspace, _ := routing.ParseWorkspaceSlug("research")
	started := time.Now().UTC()
	operations := make([]executionevidence.CompletedMCPOperation, 0, 1001)
	for index := 0; index < 1001; index++ {
		end := started.Add(time.Duration(index+1) * time.Second)
		operation, err := executionevidence.NewCompletedMCPOperation(fmt.Sprintf("mcp_%04d", index), accessidentity.Local, workspace, "docs", "write", started, end, execution.Failed, execution.MayHaveOccurred)
		if err != nil {
			t.Fatal(err)
		}
		operations = append(operations, operation)
	}
	if err := db.WriteMCPOperations(t.Context(), operations); err != nil {
		t.Fatal(err)
	}
	head, err := db.ListMCPOperations(t.Context(), workspace, 1000)
	if err != nil || len(head) != 1000 || head[len(head)-1].ID() == operations[0].ID() {
		t.Fatal("fixture did not exclude oldest retained operation")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	operation, found, err := reopened.GetMCPOperation(t.Context(), operations[0].ID())
	if err != nil || !found || operation != operations[0] {
		t.Fatalf("exact restart lookup beyond head: %#v, %v, %v", operation, found, err)
	}
	if _, found, err := reopened.GetMCPOperation(t.Context(), "mcp_%' OR 1=1 --"); err != nil || found {
		t.Fatal("MCP identity was not an exact bound query")
	}
}
