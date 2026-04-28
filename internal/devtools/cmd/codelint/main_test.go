package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandSucceedsOnCleanModule(t *testing.T) {
	t.Parallel()

	bin := buildCommand(t)
	root := t.TempDir()
	writeModuleFile(t, root, "go.mod", "module example.com/codelintcmdtest\n\ngo 1.22\n")
	writeModuleFile(t, root, "sample/sample.go", `package sample

func OK() int {
	return 1
}
`)

	cmd := exec.Command(bin, "./...")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("codelint clean module failed: %v\n%s", err, output)
	}
	if len(output) != 0 {
		t.Fatalf("clean module produced output:\n%s", output)
	}
}

func TestCommandFailsOnRepoCapViolation(t *testing.T) {
	t.Parallel()

	bin := buildCommand(t)
	root := t.TempDir()
	writeModuleFile(t, root, "go.mod", "module example.com/codelintcmdtest\n\ngo 1.22\n")
	writeModuleFile(t, root, "sample/sample.go", sampleSource("", 410, `package sample

func ok() {}
`))

	cmd := exec.Command(bin, "./...")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("codelint violating module unexpectedly succeeded:\n%s", output)
	}
	if !strings.Contains(string(output), "error:") {
		t.Fatalf("violating module output = %q, want error", output)
	}
}

func buildCommand(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "codelint")
	cmd := exec.Command("go", "build", "-o", bin, "./internal/devtools/cmd/codelint")
	cmd.Dir = repoRoot(t)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build codelint failed: %v\n%s", err, output)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", "..", "..", ".."))
}

func writeModuleFile(t *testing.T, root, relPath, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", relPath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", relPath, err)
	}
}

func sampleSource(prefix string, padLines int, body string) string {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(body)
	for i := 0; i < padLines; i++ {
		b.WriteString("// filler\n")
	}
	return b.String()
}
