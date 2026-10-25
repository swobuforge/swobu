package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/nodefile"
)

func TestOfflineConfigValidationChecksBudgetPolicyFromSingleNodeSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces:\n  research:\n    routes: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	node, err := nodefile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	validate := func(want bool) {
		t.Helper()
		out.Reset()
		errs.Reset()
		code := runConfig(context.Background(), &out, &errs, []string{"validate", "--config", path})
		if (code == ExitHealthy) != want {
			t.Fatalf("success=%v wanted=%v err=%s", code == ExitHealthy, want, errs.String())
		}
	}
	validate(true)
	if err := node.PublishAccounting([]byte(`{"schema_version":1,"limits":{"research":{"currency":"USD","amount_cents":100,"window":"day","mode":"stop_new_billable_work","unknown_price":"reject"}}}`)); err != nil {
		t.Fatal(err)
	}
	validate(true)
	if !strings.Contains(out.String(), "1 budget policies") || !strings.Contains(out.String(), "Node revision ") {
		t.Fatal("missing policy and full revision", out.String())
	}
	if err := node.PublishAccounting([]byte(`{"schema_version":1,"limits":{"research":{"currency":"USD","amount_cents":0,"window":"day","mode":"stop_new_billable_work","unknown_price":"reject"}}}`)); err != nil {
		t.Fatal(err)
	}
	validate(false)
	if !strings.Contains(errs.String(), "accounting") {
		t.Fatal("invalid budget escaped validator", errs.String())
	}
	if err := node.PublishAccounting([]byte(`{"schema_version":1,"limits":{}}`)); err != nil {
		t.Fatal(err)
	}
	validate(true)
	sidecar := filepath.Join(dir, "accounting.json")
	if err := os.WriteFile(sidecar, []byte(`{"limits":{"research":{"currency":"USD","amount_cents":100,"window":"day","mode":"stop_new_billable_work","unknown_price":"reject"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	validate(false)
	if !strings.Contains(errs.String(), "obsolete accounting.json") {
		t.Fatal("old second policy owner accepted", errs.String())
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("offline validation created lock %v", err)
	}
}
