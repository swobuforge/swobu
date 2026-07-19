package replay

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCanonicalContainerContractsStayMinimal(t *testing.T) {
	t.Parallel()

	assertFields := func(name string, typ reflect.Type, want []string) {
		t.Helper()
		if typ.NumField() != len(want) {
			t.Fatalf("%s has %d fields, want %d", name, typ.NumField(), len(want))
		}
		for idx, fieldName := range want {
			if typ.Field(idx).Name != fieldName {
				t.Fatalf("%s field %d = %q, want %q", name, idx, typ.Field(idx).Name, fieldName)
			}
		}
	}

	assertFields("Record", reflect.TypeOf(Record{}), []string{"Request", "Response", "CreatedAt", "ExpiresAt"})
	assertFields("Prepared", reflect.TypeOf(Prepared{}), []string{"Semantic", "Delta"})
}

func TestProductionHasNoSupersededContinuationFossils(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	forbidden := []string{
		"TurnRef",
		"PreviousResponseID",
		"replay.ID",
		"NativeContinuation",
		"ContinuationSource",
		"CaptureContinuation",
		"Record.Native",
		"ProviderEncodeInput.NativeContinuation",
		"EventMetadataFields.Raw",
		"map[compat.Feature]any",
	}
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, fossil := range forbidden {
			if strings.Contains(string(raw), fossil) {
				t.Fatalf("production fossil %q remains in %s", fossil, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production sources: %v", err)
	}
}
