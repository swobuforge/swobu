package executionevidence

import (
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestCompletedMCPOperationRejectsContradictorySettlement(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("research")
	started := time.Now().UTC()
	for _, occurrence := range []execution.Occurrence{execution.MayHaveOccurred, execution.DidNotOccur} {
		if _, err := NewCompletedMCPOperation("mcp_test", accessidentity.Local, slug, "docs", "search", started, started.Add(time.Second), execution.Succeeded, occurrence); err == nil {
			t.Fatal("contradictory success accepted")
		}
	}
	if err := (CompletedMCPOperation{}).Validate(); err == nil {
		t.Fatal("zero operation accepted")
	}
	if _, err := NewCompletedMCPOperation("mcp_test", accessidentity.Local, slug, "docs", "search", started, started.Add(-time.Second), execution.Failed, execution.MayHaveOccurred); err == nil {
		t.Fatal("negative duration accepted")
	}
}
