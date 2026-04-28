package rolelint

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestDominantExportedType_UsesOwnedLOC(t *testing.T) {
	t.Parallel()

	src := `package sample
type CommandErrorCode string
type OperatorEndpointStore struct{}

func (OperatorEndpointStore) List() {}
func (OperatorEndpointStore) Put() {}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "operator_endpoint_store.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("ParseFile returned error: %v", err)
	}

	got, ok := dominantExportedType(fset, file)
	if !ok {
		t.Fatal("dominantExportedType returned no result")
	}
	if got != "OperatorEndpointStore" {
		t.Fatalf("dominant type = %q, want %q", got, "OperatorEndpointStore")
	}
}

func TestHasDominantFilenameTokenOverlap_RequiresSharedToken(t *testing.T) {
	t.Parallel()

	if !hasDominantFilenameTokenOverlap("resolved_routable_target.go", "ResolvedRoutableTarget") {
		t.Fatal("expected shared token overlap")
	}
	if hasDominantFilenameTokenOverlap("projection.go", "ResolvedRoutableTarget") {
		t.Fatal("expected no token overlap")
	}
}
