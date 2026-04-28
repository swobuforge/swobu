package executionledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheck_FailsWhenP0TaskMissingScopeAndClaims(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tasks/ready/00-reimplementation-backlog.md", "31a. `06-proof-release/30d.md` — **P0 PENDING**")
	writeFile(t, root, "tasks/ready/06-proof-release/30d.md", "## Title\n\nx\n")

	diagnostics, err := Check(root)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(diagnostics) == 0 {
		t.Fatal("diagnostics = 0, want failures")
	}
}

func TestCheck_FailsWhenBacklogDoneButVerdictNotDoneScoped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tasks/ready/00-reimplementation-backlog.md", "31a. `06-proof-release/30d.md` — **P0 DONE**")
	writeFile(t, root, "tasks/ready/06-proof-release/30d.md", `## Scope Verdict

- verdict: `+"`proven`"+`
- done scope: x
- not-done scope: y

## Claim Ledger

- [x] claim: c1 | proof: `+"`go test ./...`"+` | status: `+"`proven`"+` | validated: `+"`2026-04-21`"+`
`)

	diagnostics, err := Check(root)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("len(diagnostics) = %d, want 1", len(diagnostics))
	}
	if !strings.Contains(diagnostics[0].Message, "done_scoped") {
		t.Fatalf("message = %q, want done_scoped rule", diagnostics[0].Message)
	}
}

func TestCheck_PassesForValidDoneScopedTask(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tasks/ready/00-reimplementation-backlog.md", "31a. `06-proof-release/30d.md` — **P0 DONE**")
	writeFile(t, root, "tasks/ready/06-proof-release/30d.md", `## Scope Verdict

- verdict: `+"`done_scoped`"+`
- done scope: declared matrix + contract gates
- not-done scope: provider breadth expansion beyond current band

## Claim Ledger

- [x] claim: declared protocol matrix is executable | proof: `+"`go test ./test/integration/providers -run TestProviderCatalog_DeclaredProtocolsAreExecutable -count=1`"+` | status: `+"`proven`"+` | validated: `+"`2026-04-21`"+`
- [x] claim: conformance matrix is green for declared providers/families | proof: `+"`go test ./test/compatibility/runtime/openai ./test/compatibility/runtime/anthropic -count=1`"+` | status: `+"`proven`"+` | validated: `+"`2026-04-21`"+`
`)

	diagnostics, err := Check(root)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diagnostics)
	}
}

func TestBuildReport(t *testing.T) {
	report := BuildReport([]Task{{
		Path:          "tasks/ready/06-proof-release/30d.md",
		BacklogStatus: BacklogStatusPending,
		Tags:          []string{"P0"},
		Verdict:       VerdictInProgress,
		Claims: []Claim{
			{Status: ClaimStatusProven},
			{Status: ClaimStatusImplemented},
		},
	}})
	if !strings.Contains(report, "1/2") {
		t.Fatalf("report = %q, want proven count", report)
	}
}

func TestLoadBacklogTasks_FilterByTagAndPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tasks/ready/00-reimplementation-backlog.md", strings.Join([]string{
		"31a. `06-proof-release/30d.md` — **P0 PENDING**",
		"31b. `07-execution-system/41.md` — **TRACKED PENDING**",
	}, "\n"))
	taskText := `## Scope Verdict

- verdict: ` + "`in_progress`" + `
- done scope: x
- not-done scope: y

## Claim Ledger

- [ ] claim: c1 | proof: ` + "`go test ./...`" + ` | status: ` + "`in_progress`" + ` | validated: ` + "`2026-04-21`" + `
`
	writeFile(t, root, "tasks/ready/06-proof-release/30d.md", taskText)
	writeFile(t, root, "tasks/ready/07-execution-system/41.md", taskText)

	got, err := LoadBacklogTasks(root, TaskFilter{
		RequireAnyTag: []string{"TRACKED"},
		PathPrefix:    "07-execution-system",
	})
	if err != nil {
		t.Fatalf("LoadBacklogTasks returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(got))
	}
	if got[0].Path != "tasks/ready/07-execution-system/41.md" {
		t.Fatalf("path = %q, want execution-system task", got[0].Path)
	}
}

func writeFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", relPath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", relPath, err)
	}
}
