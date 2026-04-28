package docpolicy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoverningDocsFromTask(t *testing.T) {
	text := `## Title

Example

## Governing Docs

- docs/04-design/public-interface-contracts.md section 4
- docs/04-design/public-interface-contracts.md section 5
- docs/04-design/public-interface-contracts/operator-cli-and-cockpit-contracts.md section 5.3

## Tests
`
	got := governingDocsFromTask(text)
	want := []string{
		"docs/04-design/public-interface-contracts.md",
		"docs/04-design/public-interface-contracts/operator-cli-and-cockpit-contracts.md",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("governingDocsFromTask() = %v, want %v", got, want)
	}
}

func TestCheckTaskGoverningDocPointersWarnsForDecomposedParentDoc(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "AGENTS.md", "# Swobu Agent Instructions\n")
	mustWriteFile(t, root, "docs/README.md", "# Docs\n")
	mustWriteFile(t, root, "docs/04-design/public-interface-contracts.md", "# Parent\n")
	mustWriteFile(t, root, "docs/04-design/public-interface-contracts/operator-cli-and-cockpit-contracts.md", "# Child\n")
	mustWriteFile(t, root, "tasks/ready/05-tui/24-example.md", `## Title

Example

## Governing Docs

- docs/04-design/public-interface-contracts.md section 5

## Tests
`)

	diagnostics := checkTaskGoverningDocPointers(root)
	if len(diagnostics) != 1 {
		t.Fatalf("len(diagnostics) = %d, want 1", len(diagnostics))
	}
	if !diagnostics[0].Warning {
		t.Fatalf("expected warning diagnostic, got %+v", diagnostics[0])
	}
	if !strings.Contains(diagnostics[0].Message, "decomposed parent index") {
		t.Fatalf("unexpected diagnostic message: %q", diagnostics[0].Message)
	}
}

func TestCheckTaskGoverningDocPointersAllowsLeafDoc(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "AGENTS.md", "# Swobu Agent Instructions\n")
	mustWriteFile(t, root, "docs/README.md", "# Docs\n")
	mustWriteFile(t, root, "docs/04-design/public-interface-contracts.md", "# Parent\n")
	mustWriteFile(t, root, "docs/04-design/public-interface-contracts/operator-cli-and-cockpit-contracts.md", "# Child\n")
	mustWriteFile(t, root, "tasks/ready/04-runtime-cli/20-example.md", `## Title

Example

## Governing Docs

- docs/04-design/public-interface-contracts/operator-cli-and-cockpit-contracts.md section 5.3

## Tests
`)

	diagnostics := checkTaskGoverningDocPointers(root)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diagnostics)
	}
}

func TestCheckTaskBuildVsBuyEvidenceFailsWhenDecisionStatusMissing(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "AGENTS.md", "# Swobu Agent Instructions\n")
	mustWriteFile(t, root, "tasks/ready/05-tui/24-example.md", `## Title

Example

## Build vs Buy

- product-owned semantics: flow semantics
- commodity mechanics: clipboard I/O
- library candidates: golang.design/x/clipboard
- recommendation: use library
`)

	diagnostics := checkTaskBuildVsBuyEvidence(root)
	if len(diagnostics) == 0 {
		t.Fatalf("expected build-vs-buy diagnostics")
	}
	if !strings.Contains(diagnostics[0].Message, "decision status") {
		t.Fatalf("unexpected diagnostic message: %q", diagnostics[0].Message)
	}
}

func TestCheckTaskBuildVsBuyEvidenceAllowsDecidedStatus(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "AGENTS.md", "# Swobu Agent Instructions\n")
	mustWriteFile(t, root, "tasks/ready/07-execution-system/34-example.md", `## Title

Example

## Build vs Buy

- product-owned semantics: policy gate
- commodity mechanics: static file scanning
- library candidates: none
- recommendation: keep policy local
- decision status: `+"`decided`"+`
`)

	diagnostics := checkTaskBuildVsBuyEvidence(root)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diagnostics)
	}
}

func TestCheckExecutionLedgerReportsP0ClaimContractViolations(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "AGENTS.md", "# Swobu Agent Instructions\n")
	mustWriteFile(t, root, "docs/README.md", "# Docs\n")
	mustWriteFile(t, root, "tasks/ready/00-reimplementation-backlog.md", "31a. `06-proof-release/30d.md` — **P0 PENDING**\n")
	mustWriteFile(t, root, "tasks/ready/06-proof-release/30d.md", "## Title\n\nx\n")

	diagnostics := checkExecutionLedger(root)
	if len(diagnostics) == 0 {
		t.Fatal("diagnostics = 0, want execution-ledger failures")
	}
}

func TestCheckReleaseNoteDisciplineReportsMissingMarkers(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "AGENTS.md", "# Swobu Agent Instructions\n")
	mustWriteFile(t, root, "docs/05-engineering/release-versioning-and-migration/release-gates-rollout-and-rollback.md", "# Release\n")
	mustWriteFile(t, root, "docs/05-engineering/release-versioning-and-migration/breaking-changes-and-support-bands.md", "# Support\n")

	diagnostics := checkReleaseNoteDiscipline(root)
	if len(diagnostics) == 0 {
		t.Fatal("diagnostics = 0, want release-note-discipline failures")
	}
}

func TestCheckReleaseNoteDisciplineAcceptsRequiredMarkers(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "AGENTS.md", "# Swobu Agent Instructions\n")
	mustWriteFile(t, root, "docs/05-engineering/release-versioning-and-migration/release-gates-rollout-and-rollback.md", strings.Join([]string{
		"# Release",
		"| **Release note** | required |",
		"## Release note standard",
		"Required sections: Scope, Added, Support statement",
	}, "\n"))
	mustWriteFile(t, root, "docs/05-engineering/release-versioning-and-migration/breaking-changes-and-support-bands.md", strings.Join([]string{
		"# Support",
		"## 8.2 Release-note rule",
		"## 8.3 Support claim rule",
	}, "\n"))

	diagnostics := checkReleaseNoteDiscipline(root)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diagnostics)
	}
}

func mustWriteFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", relPath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", relPath, err)
	}
}
