package continuity

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

	assertFields("Checkpoint", reflect.TypeOf(Checkpoint{}), []string{"ResponseID", "ThreadID", "HistoryScheme", "History", "Request", "Response", "CreatedAt", "ExpiresAt"})
	assertFields("ResolvedRequest", reflect.TypeOf(ResolvedRequest{}), []string{"request", "previousHistory"})
	assertFields("previousHistory", reflect.TypeOf(previousHistory{}), []string{"response", "omitItems"})
	assertFields("requestItemRange", reflect.TypeOf(requestItemRange{}), []string{"start", "end"})

	store := reflect.TypeOf((*Store)(nil)).Elem()
	wantMethods := []string{"AdvanceThread", "GetCheckpoint", "GetThread", "IsCurrentHead", "ResolveHeadByHistory", "StartThread"}
	if store.NumMethod() != len(wantMethods) {
		t.Fatalf("Store has %d methods, want %d", store.NumMethod(), len(wantMethods))
	}
	for idx, methodName := range wantMethods {
		if store.Method(idx).Name != methodName {
			t.Fatalf("Store method %d = %q, want %q", idx, store.Method(idx).Name, methodName)
		}
	}
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
		"map[canonical.CapabilityPath]any",
		"ParentContinuation",
		"ContinuationChain",
		"Chain(ctx",
		"nativeDelta",
		"rebaseAttemptMedia",
		"historicalMediaForAttempt",
		"ShiftItems(",
		"ResumeHistory",
		"FindByHistory",
		"ResolvedMedia",
		"HistoricalMedia",
		"ResolveMedia",
		"WithSameItemLayout",
		"ReplaceWithFullHistory",
		"ClientSessionID",
		"StartSession",
		"AdvanceSession",
		"ErrSessionExists",
		"ErrStaleSessionHead",
		"ErrSessionSchemeMismatch",
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
