package artifactpolicy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheck_FlagsUnreferencedArtifact(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "test/compatibility/surface/tui/testdata/wireframes/clients/Z-01_demo.txt", "fixture")
	writeFile(t, root, "test/compatibility/surface/tui/s_series_source_sync_contract_test.go", "no references here")

	diagnostics, err := Check(root)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("len(diagnostics) = %d, want 1", len(diagnostics))
	}
	if got, want := diagnostics[0].Filename, "test/compatibility/surface/tui/testdata/wireframes/clients/Z-01_demo.txt"; got != want {
		t.Fatalf("diagnostic filename = %q, want %q", got, want)
	}
}

func TestCheck_AllowsReferencedArtifact(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "test/compatibility/surface/tui/testdata/wireframes/clients/Z-01_demo.txt", "fixture")
	writeFile(t, root, "test/compatibility/surface/tui/s_series_source_sync_contract_test.go", `func _() { assertWireframeEqualsFixture(nil, "", "Z-01_demo.txt") }`)

	diagnostics, err := Check(root)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diagnostics)
	}
}

func TestCheck_IgnoresSelfReferenceInsideArtifactFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "test/compatibility/surface/tui/testdata/wireframes/clients/Z-01_demo.txt", "Z-01_demo.txt")

	diagnostics, err := Check(root)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("len(diagnostics) = %d, want 1", len(diagnostics))
	}
}

func TestCheckWithClasses_SupportsCustomClass(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "notes/artifacts/A-01.txt", "fixture")
	writeFile(t, root, "README.md", "A-01.txt")

	diagnostics, err := CheckWithClasses(root, []Class{{
		Name:       "note artifact",
		Root:       "notes/artifacts",
		Extensions: []string{".txt"},
	}})
	if err != nil {
		t.Fatalf("CheckWithClasses() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diagnostics)
	}
}

func TestCheckWithClasses_FlagsMissingProvenanceSidecar(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "test/compatibility/runtime/openai/testdata/a.sse", "data: ok\n")
	writeFile(t, root, "test/compatibility/runtime/openai/custom_contract_test.go", `"a.sse"`)

	diagnostics, err := CheckWithClasses(root, []Class{{
		Name:              "provider response replay artifact",
		Root:              "test/compatibility/runtime",
		Extensions:        []string{".sse"},
		PathContains:      "/testdata/",
		RequireProvenance: true,
	}})
	if err != nil {
		t.Fatalf("CheckWithClasses() error = %v", err)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("len(diagnostics) = %d, want 1", len(diagnostics))
	}
	if diagnostics[0].Message == "" || diagnostics[0].Filename == "" {
		t.Fatalf("unexpected diagnostic: %+v", diagnostics[0])
	}
}

func TestCheckWithClasses_AllowsVerbatimProvenanceFixture(t *testing.T) {
	root := t.TempDir()
	recordBody := "data: one\n\ndata: two\n"
	writeFile(t, root, "test/fixtures/live_matrix/records/record.json", `{"response":{"body":"data: one\n\ndata: two\n"}}`)
	writeFile(t, root, "test/compatibility/runtime/openai/testdata/a.sse", recordBody)
	writeFile(t, root, "test/compatibility/runtime/openai/testdata/a.sse.provenance.json", `{
  "source_record": "test/fixtures/live_matrix/records/record.json",
  "source_field": "response.body",
  "extraction": "verbatim"
}`)
	writeFile(t, root, "test/compatibility/runtime/openai/custom_contract_test.go", `"a.sse"`)

	diagnostics, err := CheckWithClasses(root, []Class{{
		Name:              "provider response replay artifact",
		Root:              "test/compatibility/runtime",
		Extensions:        []string{".sse"},
		PathContains:      "/testdata/",
		ExcludeFileSuffix: []string{".provenance.json"},
		RequireProvenance: true,
	}})
	if err != nil {
		t.Fatalf("CheckWithClasses() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diagnostics)
	}
}

func writeFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", relPath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", relPath, err)
	}
}
