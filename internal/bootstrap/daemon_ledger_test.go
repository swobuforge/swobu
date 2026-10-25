package bootstrap

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	domain "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestNodeActivityReportsCaptureGapWithoutWorkspaceCounterLeak(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "sqlite")
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(t.Context(), StartInput{ConfigPath: filepath.Join(root, "swobu.yaml"), StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	client := operatorclient.New(http.DefaultClient, daemon.BaseURL())
	projection, err := client.Activity(t.Context(), "")
	if err != nil || projection.History == nil || projection.History.State != ledger.StateHealthy {
		t.Fatalf("Node capture health=%#v, %v", projection, err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	identity, err := daemon.executionEvidenceStore.BeginMCP(accessidentity.Local, slug, "docs", "search")
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.executionEvidenceStore.SettleMCP(identity, execution.Failed, execution.MayHaveOccurred); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for daemon.ledgerWriter.Health().CommittedMCPOperationsTotal != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if daemon.ledgerWriter.Health().CommittedMCPOperationsTotal != 1 {
		t.Fatal("capture did not commit")
	}
	if daemon.ledgerWriter.OfferMCP(domain.CompletedMCPOperation{}) {
		t.Fatal("invalid record entered capture queue")
	}
	projection, err = client.Activity(t.Context(), "")
	if err != nil || projection.History == nil || projection.History.State != ledger.StateDegraded || projection.History.LastError != ledger.ErrorHistoryIncomplete || projection.History.OfferedMCPOperationsTotal != 2 || projection.History.CommittedMCPOperationsTotal != 1 || projection.History.DroppedMCPOperationsTotal != 1 || len(projection.MCPOperations) != 1 {
		t.Fatalf("capture gap hidden by retained history=%#v, %v", projection, err)
	}
	workspace, err := client.Activity(t.Context(), "research")
	if err != nil || workspace.History != nil || len(workspace.MCPOperations) != 1 {
		t.Fatalf("Workspace counter leakage=%#v, %v", workspace, err)
	}
	// The canonical Activity read is also what the Web Operator consumes;
	// it must preserve the Node capture gap without leaking it to Workspace scope.
	if workspace.MCPOperations[0].OperationID != identity {
		t.Fatalf("Workspace Activity changed the retained operation: %#v", workspace.MCPOperations)
	}
}

func TestDaemonLedgerSettlementDrainsAndSurvivesRestart(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "sqlite")
	t.Setenv("SWOBU_LEDGER_MAX_BYTES", "")
	t.Setenv("SWOBU_LEDGER_DETAIL_RETENTION_DAYS", "")
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "swobu.yaml")
	input := StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}}
	daemon, err := Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	slug, _ := routing.ParseWorkspaceSlug("research")
	identity, err := daemon.executionEvidenceStore.BeginMCP(accessidentity.Local, slug, "docs", "search")
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.executionEvidenceStore.SettleMCP(identity, execution.Failed, execution.MayHaveOccurred); err != nil {
		t.Fatal(err)
	}
	live := daemon.executionEvidenceStore.ProjectActivity(evidence.ProjectionInput{}).MCPOperations[0]
	deadline := time.Now().Add(time.Second)
	for daemon.ledgerWriter.Health().CommittedMCPOperationsTotal != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if daemon.ledgerWriter.Health().CommittedMCPOperationsTotal != 1 {
		t.Fatal("capture did not commit before merge proof")
	}
	merged, err := daemon.activity(t.Context(), evidence.ProjectionScope{Kind: evidence.ProjectionScopeWorkspace, Workspace: "research"})
	if err != nil || len(merged.MCPOperations) != 1 || merged.MCPOperations[0] != live {
		t.Fatalf("duplicate memory/durable identity=%#v, %v", merged, err)
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	if health := daemon.ledgerWriter.Health(); health.CommittedMCPOperationsTotal != 1 || health.DroppedMCPOperationsTotal != 0 {
		t.Fatalf("shutdown health=%#v", health)
	}
	restarted, err := Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	operations, err := restarted.ledgerStore.ListMCPOperations(t.Context(), slug, 10)
	if err != nil || len(operations) != 1 || operations[0].ID() != identity || operations[0].Occurrence() != execution.MayHaveOccurred {
		t.Fatalf("restart history=%#v, %v", operations, err)
	}
	all, err := restarted.activity(t.Context(), evidence.ProjectionScope{Kind: evidence.ProjectionScopeAll})
	if err != nil || len(all.MCPOperations) != 1 || all.MCPOperations[0].Workspace != "research" {
		t.Fatalf("Node-wide durable history=%#v, %v", all, err)
	}
}

func TestDaemonLedgerUnavailableDegradesAndOffCreatesNoDatabase(t *testing.T) {
	for _, backend := range []string{"sqlite", "off"} {
		t.Run(backend, func(t *testing.T) {
			t.Setenv("SWOBU_LEDGER", backend)
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			if backend == "sqlite" {
				if err := os.Mkdir(filepath.Join(root, "swobu.ledger.sqlite3"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			daemon, err := Start(t.Context(), StartInput{ConfigPath: filepath.Join(root, "swobu.yaml"), StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
			if err != nil {
				t.Fatal(err)
			}
			defer daemon.Close()
			status, err := daemon.Status()
			if err != nil {
				t.Fatal(err)
			}
			if backend == "sqlite" && status.State != HealthStateDegraded {
				t.Fatalf("unavailable Ledger health=%#v", status)
			}
			if backend == "sqlite" {
				if _, err := daemon.activity(t.Context(), evidence.ProjectionScope{}); err == nil {
					t.Fatal("unavailable durable history masqueraded as empty Activity")
				}
			}
			if backend == "off" {
				if daemon.ledgerWriter != nil || status.State == HealthStateDegraded {
					t.Fatalf("disabled Ledger=%#v", status)
				}
				if _, err := os.Stat(filepath.Join(root, "swobu.ledger.sqlite3")); !os.IsNotExist(err) {
					t.Fatalf("off created database: %v", err)
				}
			}
		})
	}
}
