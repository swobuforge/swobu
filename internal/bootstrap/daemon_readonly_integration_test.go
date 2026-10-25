package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestExternallyManagedDaemonCanServeWithoutConfigurationWrites(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "sqlite")
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SWOBU_HOME", t.TempDir())
	path := filepath.Join(root, "swobu.yaml")
	seed, err := configstore.OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"readonly-response","model":"fixture","output_text":"readonly-private-response"}`)
	}))
	defer upstream.Close()
	writeCurrentWorkspaceFixture(t, path, root, "research", upstream.URL, "responses", false)
	if err := os.Remove(path + ".lock"); err != nil {
		t.Fatal(err)
	}
	// Externally managed state must not require a writable configuration mount.
	if err := os.Chmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0700) })
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		probe := filepath.Join(root, "must-not-write")
		if err := os.WriteFile(probe, []byte("probe"), 0600); !errors.Is(err, os.ErrPermission) {
			_ = os.Remove(probe)
			t.Fatalf("fixture is not an immutable configuration directory: %v", err)
		}
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(context.Background(), StartInput{ConfigPath: path, ReadOnlyConfig: true, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	if !daemon.configStore.ReadOnly() {
		t.Fatal("routing writer not read-only")
	}
	runtimeDir, err := config.NodeRuntimeRoot(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if daemon.runtimeDir != runtimeDir {
		t.Fatalf("wrong runtime root %q expected %q", daemon.runtimeDir, runtimeDir)
	}
	if daemon.ledgerStore == nil {
		t.Fatal("externally managed Node lost durable Ledger")
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "ledger.sqlite3")); err != nil {
		t.Fatalf("runtime Ledger not externalized: %v", err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, daemon.BaseURL()+"/workspaces/research/responses", strings.NewReader(`{"model":"chat","input":"readonly-private-prompt","stream":false}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != 200 || !strings.Contains(string(body), "readonly-private-response") {
		t.Fatalf("read-only Node failed real execution: status=%d err=%v", response.StatusCode, err)
	}
	// Prove retained history is actually reopened, not merely that a second
	// blank database can be created at a writable path.
	workspace, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	settled := time.Now().UTC().Truncate(time.Second)
	operation, err := executionevidence.NewCompletedMCPOperation("readonly_restart", accessidentity.Local, workspace, "fixture", "call", settled.Add(-time.Second), settled, execution.Succeeded, execution.Occurred)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.ledgerStore.WriteMCPOperations(t.Context(), []executionevidence.CompletedMCPOperation{operation}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "swobu.ledger.sqlite3")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("SQLite wrote beside immutable YAML: %v", err)
	}
	if _, err := daemon.accountingStore.Reserve("research", "read-only-unlimited", 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "accounting.json")); err != nil {
		t.Fatalf("runtime accounting journal not externalized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "accounting.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("accounting journal wrote into immutable configuration mount", err)
	}
	limit := accounting.Limit{Currency: "USD", AmountCents: 10, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: "reject"}
	if err := daemon.accountingStore.SetLimit("research", limit); err == nil {
		t.Fatal("externally managed budget was mutated")
	}
	request, err = http.NewRequest(http.MethodPost, daemon.BaseURL()+"/api/v1/principals", bytes.NewReader([]byte(`{"name":"Alice"}`)))
	if err != nil {
		t.Fatal(err)
	}
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		t.Fatalf("control mutation accepted: %d", response.StatusCode)
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("Node YAML was modified", err)
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only daemon created lock: %v", err)
	}
	reopened, err := Start(context.Background(), StartInput{ConfigPath: path, ReadOnlyConfig: true, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatalf("restart of externally managed Node: %v", err)
	}
	defer reopened.Close()
	if reopened.ledgerStore == nil {
		t.Fatal("restart lost durable Ledger")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "swobu.yaml" {
		t.Fatalf("runtime wrote configuration-side files: %v %v", entries, err)
	}
	page, err := reopened.ledgerStore.ListRecentSummaries(t.Context(), ledger.ExchangeFilter{Workspace: &workspace}, ledger.Cursor{}, 10)
	if err != nil || len(page.Exchanges) != 1 {
		t.Fatalf("restart lost actual model execution: count=%d err=%v", len(page.Exchanges), err)
	}
	retained, found, err := reopened.ledgerStore.GetMCPOperation(t.Context(), "readonly_restart")
	if err != nil || !found || retained.ID() != operation.ID() {
		t.Fatalf("restart lost retained MCP evidence: found=%t err=%v", found, err)
	}
}
