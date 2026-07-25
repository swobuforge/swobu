package session

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

	assertFields("Checkpoint", reflect.TypeOf(Checkpoint{}), []string{"HistoryFingerprint", "Request", "Response", "ResolvedMedia", "CreatedAt", "ExpiresAt"})
	assertFields("ResolvedRequest", reflect.TypeOf(ResolvedRequest{}), []string{"Full", "Delta", "ResolvedMedia"})
}

func TestProductionHasNoSupersededSessionVocabulary(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	forbidden := []string{
		"ReplayStore",
		"loadingReplayPhase",
		"loadReplayCommand",
		"replayLoaded",
		"MaxReplayBytes",
		"PrepareFromRecord",
		"PrepareCurrent",
		"PreferredForTarget",
		"CommitReader",
		"TerminalCommitConfig",
		"ContinuationStore",
		"ContinuationRecord",
		"PrepareContinuation",
		"nativeContinuation",
		"continuationPrepared",
		"reduceLoadingReplay",
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
