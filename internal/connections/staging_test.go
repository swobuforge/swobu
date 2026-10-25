package connections

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

func TestStagingMetadataCommitsBeforeStorageAndRecoversRestart(t *testing.T) {
	lifecycle, store, _, value, _ := lifecycleFixture(t)
	slot := "connections/" + value.ID().String() + "/cred_" + strings.Repeat("a", 32)
	candidates := []credentialref.Ref{credentialref.Parse("secret:" + slot), credentialref.Parse("secretfile:" + slot)}
	admit := func(context.Context) error { return nil }
	called := 0
	write := func(context.Context) (credentialref.Ref, error) {
		called++
		reopened, err := openInlineStoreFixture(t, store.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(reopened.StagedCredentials()) != 2 {
			t.Fatal("storage ran without durable metadata")
		}
		return candidates[1], nil
	}
	replace := store.publishSection
	store.publishSection = func([]byte) error { return errors.New("write failure") }
	if _, err := store.StageCredential(t.Context(), value.ID(), candidates, admit, write); err == nil {
		t.Fatal("metadata write failure accepted")
	}
	if called != 0 || len(store.StagedCredentials()) != 0 {
		t.Fatal("failed staging invoked storage")
	}
	store.publishSection = replace
	selected, err := store.StageCredential(t.Context(), value.ID(), candidates, admit, write)
	if err != nil || selected != candidates[1] {
		t.Fatal("staging failed", err)
	}
	if _, err := store.StageCredential(t.Context(), value.ID(), candidates, admit, write); err == nil {
		t.Fatal("slot reused")
	}
	if called != 1 {
		t.Fatal("duplicate staging overwrote slot")
	}
	reaped := 0
	if err := lifecycle.ReconcileStagedCredentials(t.Context(), time.Now(), func(context.Context, connectionidentity.ID, credentialref.Ref) error { reaped++; return nil }); err != nil {
		t.Fatal(err)
	}
	if reaped != 0 {
		t.Fatal("unexpired stage reclaimed")
	}
	if err := lifecycle.ReconcileStagedCredentials(t.Context(), time.Now().Add(48*time.Hour), func(context.Context, connectionidentity.ID, credentialref.Ref) error { reaped++; return nil }); err != nil {
		t.Fatal(err)
	}
	reopened, err := openInlineStoreFixture(t, store.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reaped != 2 || len(reopened.StagedCredentials()) != 0 || len(reopened.retired) != 2 {
		t.Fatal("abandoned stage not durably reclaimed")
	}
}

func TestStagedReconciliationPreservesLiveMaterialAndRetriesStorageFailure(t *testing.T) {
	lifecycle, store, _, value, _ := lifecycleFixture(t)
	ref := credentialref.Parse("secretfile:connections/" + value.ID().String() + "/cred_" + strings.Repeat("b", 32))
	admit := func(context.Context) error { return nil }
	if _, err := store.StageCredential(t.Context(), value.ID(), []credentialref.Ref{ref}, admit, func(context.Context) (credentialref.Ref, error) { return ref, nil }); err != nil {
		t.Fatal(err)
	}
	// The fixture timestamp can be later than wall clock; choose a valid update.
	next, err := value.ReplaceCredential(value.Revision(), "", credentialref.Parse("env:MCP_TOKEN"), ref, value.UpdatedAt().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(next, value.Revision()); err != nil {
		t.Fatal(err)
	}
	called := 0
	retire := func(context.Context, connectionidentity.ID, credentialref.Ref) error {
		called++
		return errors.New("storage offline")
	}
	if err := lifecycle.ReconcileStagedCredentials(t.Context(), time.Now().Add(48*time.Hour), retire); err != nil {
		t.Fatal(err)
	}
	if called != 0 || len(store.StagedCredentials()) != 1 {
		t.Fatal("live material reaped")
	}
	if err := lifecycle.DeleteIfUnused(t.Context(), next.ID(), next.Revision()); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.ReconcileStagedCredentials(t.Context(), time.Now().Add(48*time.Hour), retire); err == nil {
		t.Fatal("storage failure hidden")
	}
	if called != 1 || len(store.StagedCredentials()) != 1 {
		t.Fatal("cleanup retry metadata lost")
	}
	if err := lifecycle.ReconcileStagedCredentials(t.Context(), time.Now().Add(48*time.Hour), func(context.Context, connectionidentity.ID, credentialref.Ref) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(store.StagedCredentials()) != 0 {
		t.Fatal("successful cleanup retained pending entry")
	}
}

func TestStagingStorageFailureAndInvalidSelectionRetainRecoveryMetadata(t *testing.T) {
	_, store, _, value, _ := lifecycleFixture(t)
	admit := func(context.Context) error { return nil }
	for _, suffix := range []string{"c", "d"} {
		ref := credentialref.Parse("secretfile:connections/" + value.ID().String() + "/cred_" + strings.Repeat(suffix, 32))
		_, err := store.StageCredential(t.Context(), value.ID(), []credentialref.Ref{ref}, admit, func(context.Context) (credentialref.Ref, error) {
			if suffix == "c" {
				return credentialref.Ref{}, errors.New("backend includes synthetic-sensitive-detail")
			}
			return credentialref.Parse("env:UNREGISTERED"), nil
		})
		if err == nil || strings.Contains(err.Error(), "synthetic-sensitive-detail") {
			t.Fatal("storage failure/selection validation leaked or accepted")
		}
	}
	reopened, err := openInlineStoreFixture(t, store.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.StagedCredentials()) != 2 {
		t.Fatal("failed storage lost recovery metadata")
	}
}

func TestStagedCleanupBackendFailureDoesNotStarveOtherCandidates(t *testing.T) {
	lifecycle, store, _, value, _ := lifecycleFixture(t)
	slot := "connections/" + value.ID().String() + "/cred_" + strings.Repeat("e", 32)
	keyring := credentialref.Parse("secret:" + slot)
	file := credentialref.Parse("secretfile:" + slot)
	if _, err := store.StageCredential(t.Context(), value.ID(), []credentialref.Ref{keyring, file}, func(context.Context) error { return nil }, func(context.Context) (credentialref.Ref, error) { return file, nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := lifecycle.ReconcileStagedCredentials(t.Context(), time.Now().Add(48*time.Hour), func(_ context.Context, _ connectionidentity.ID, ref credentialref.Ref) error {
		calls++
		if ref.Kind() == credentialref.KindSecret {
			return errors.New("keyring unavailable")
		}
		return nil
	})
	if err == nil || calls != 2 {
		t.Fatal("unavailable backend starved independent cleanup")
	}
	stages := store.StagedCredentials()
	if len(stages) != 1 || stages[0].Reference != keyring.String() {
		t.Fatal("cleanup did not retain only failed candidate")
	}
}

func TestStagingRetryReservesOnceAndRecoversUncertainStorage(t *testing.T) {
	_, store, _, value, _ := lifecycleFixture(t)
	slot := "connections/" + value.ID().String() + "/cred_" + strings.Repeat("b", 32)
	refs := []credentialref.Ref{credentialref.Parse("secret:" + slot), credentialref.Parse("secretfile:" + slot)}
	allocations, writes := 0, 0
	allocate := func() ([]credentialref.Ref, error) { allocations++; return refs, nil }
	admit := func(context.Context) error { return nil }
	uncertain := errors.New("synthetic storage acknowledgement lost")
	write := func(_ context.Context, candidates []credentialref.Ref, fresh bool) (credentialref.Ref, error) {
		writes++
		if len(candidates) != 2 {
			t.Fatal("lost backend candidate")
		}
		if writes == 1 {
			if !fresh {
				t.Fatal("initial allocation was not fresh")
			}
			return credentialref.Ref{}, uncertain
		}
		if fresh {
			t.Fatal("retry treated uncertain storage as fresh")
		}
		return refs[1], nil
	}
	if _, err := store.StageCredentialRetry(t.Context(), value.ID(), value.Revision(), "", "local", "retry", admit, allocate, write); !errors.Is(err, uncertain) {
		t.Fatal(err)
	}
	reopened, err := openInlineStoreFixture(t, store.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := reopened.StageCredentialRetry(t.Context(), value.ID(), value.Revision(), "", "local", "retry", admit, allocate, write)
	if err != nil || selected != refs[1] || allocations != 1 {
		t.Fatal("restart allocated new material", err)
	}
	if _, err := reopened.StageCredentialRetry(t.Context(), value.ID(), value.Revision(), "different-binding", "local", "retry", admit, allocate, write); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("changed intent accepted")
	}
	denied := errors.New("denied")
	if _, err := reopened.StageCredentialRetry(t.Context(), value.ID(), value.Revision(), "", "local", "retry", func(context.Context) error { return denied }, allocate, write); !errors.Is(err, denied) {
		t.Fatal("retry not authorized")
	}
	if writes != 2 {
		t.Fatal("rejected retry invoked storage")
	}
}

func TestStagingRetryPublicationFailureNeverInvokesStorage(t *testing.T) {
	_, store, _, value, _ := lifecycleFixture(t)
	slot := "connections/" + value.ID().String() + "/cred_" + strings.Repeat("c", 32)
	store.publishSection = func([]byte) error { return errors.New("synthetic failure") }
	called := false
	_, err := store.StageCredentialRetry(t.Context(), value.ID(), value.Revision(), "", "local", "retry", func(context.Context) error { return nil }, func() ([]credentialref.Ref, error) {
		return []credentialref.Ref{credentialref.Parse("secretfile:" + slot)}, nil
	}, func(context.Context, []credentialref.Ref, bool) (credentialref.Ref, error) {
		called = true
		return credentialref.Ref{}, nil
	})
	if err == nil || called || len(store.StagedCredentials()) != 0 {
		t.Fatal("failed reservation reached storage")
	}
}

func TestStagingRetryRejectsCorruptGroupingAndContainsNoMaterial(t *testing.T) {
	_, store, _, value, _ := lifecycleFixture(t)
	slot := "connections/" + value.ID().String() + "/cred_" + strings.Repeat("d", 32)
	refs := []credentialref.Ref{credentialref.Parse("secret:" + slot), credentialref.Parse("secretfile:" + slot)}
	_, err := store.StageCredentialRetry(t.Context(), value.ID(), value.Revision(), "", "local", "private-retry-key", func(context.Context) error { return nil }, func() ([]credentialref.Ref, error) { return refs, nil }, func(context.Context, []credentialref.Ref, bool) (credentialref.Ref, error) { return refs[1], nil })
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-retry-key") {
		t.Fatal("registry retained raw retry key")
	}
	corrupt := make(map[string]StagedCredential)
	for key, stage := range store.staged {
		corrupt[key] = stage
	}
	changed := corrupt[refs[1].String()]
	changed.Intent = strings.Repeat("e", 64)
	corrupt[refs[1].String()] = changed
	if err := validateStaging(corrupt); err == nil {
		t.Fatal("accepted divergent retry candidate intent")
	}
	changed = store.staged[refs[1].String()]
	changed.RetryKey = "malformed"
	corrupt[refs[1].String()] = changed
	if err := validateStaging(corrupt); err == nil {
		t.Fatal("accepted malformed retry key digest")
	}
}

func TestCommittedReplacementStageLeaseCoversReceiptReplay(t *testing.T) {
	lifecycle, store, _, value, _ := lifecycleFixture(t)
	slot := "connections/" + value.ID().String() + "/cred_" + strings.Repeat("f", 32)
	refs := []credentialref.Ref{credentialref.Parse("secret:" + slot), credentialref.Parse("secretfile:" + slot)}
	admit := func(context.Context) error { return nil }
	selected, err := store.StageCredentialRetry(t.Context(), value.ID(), value.Revision(), "", "local", "retry", admit, func() ([]credentialref.Ref, error) { return refs, nil }, func(context.Context, []credentialref.Ref, bool) (credentialref.Ref, error) { return refs[1], nil })
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a slow/recovered storage transaction near the end of its lease.
	for key, stage := range store.staged {
		stage.ExpiresAt = time.Now().Add(time.Minute)
		store.staged[key] = stage
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	previous := store.publishSection
	store.publishSection = func([]byte) error { return errors.New("synthetic failure") }
	if _, _, err := store.ReplaceStagedCredentialChecked(t.Context(), value.ID(), value.Revision(), "", selected, "local", "retry", admit); err == nil {
		t.Fatal("accepted failed commit")
	}
	after, _ := os.ReadFile(store.path)
	if string(before) != string(after) {
		t.Fatal("failed commit changed durable leases")
	}
	store.publishSection = previous
	_, _, err = store.ReplaceStagedCredentialChecked(t.Context(), value.ID(), value.Revision(), "", selected, "local", "retry", admit)
	if err != nil {
		t.Fatal(err)
	}
	var expiry time.Time
	for _, receipt := range store.replacements {
		expiry = receipt.ExpiresAt
	}
	for _, stage := range store.StagedCredentials() {
		if stage.ExpiresAt.Before(expiry) {
			t.Fatal("staging cleanup can expire before committed retry receipt")
		}
	}
	retired := 0
	if err := lifecycle.ReconcileStagedCredentials(t.Context(), time.Now().Add(2*time.Minute), func(context.Context, connectionidentity.ID, credentialref.Ref) error { retired++; return nil }); err != nil {
		t.Fatal(err)
	}
	if retired != 0 {
		t.Fatal("cleanup consumed still-replayable candidates")
	}
	reopened, err := openInlineStoreFixture(t, store.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reopened.StageCredentialRetry(t.Context(), value.ID(), value.Revision(), "", "local", "retry", admit, func() ([]credentialref.Ref, error) { t.Fatal("replay allocated"); return nil, nil }, func(_ context.Context, got []credentialref.Ref, fresh bool) (credentialref.Ref, error) {
		if fresh || len(got) != 2 {
			t.Fatal("lost retry candidates")
		}
		return selected, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReplacementTracksPreexistingOwnedBindingForCleanupAfterRestart(t *testing.T) {
	_, store, workspaces, base, _ := lifecycleFixture(t)
	old := credentialref.Parse("secretfile:connections/" + base.ID().String() + "/cred_" + strings.Repeat("a", 32))
	spec, err := NewMCPHTTPSpec(base.Spec().(MCPHTTPSpec).Endpoint(), old)
	if err != nil {
		t.Fatal(err)
	}
	// This imported binding has no staging metadata. Keep timestamps before the
	// checked command's real clock instead of introducing a future edit fixture.
	value, err := base.EditSameIdentity(base.Revision(), base.DisplayName(), spec, time.Now().Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(value, base.Revision()); err != nil {
		t.Fatal(err)
	}
	sharedID, _ := connectionidentity.New()
	shared, err := New(sharedID, "Shared old material", spec, time.Now().Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(shared, Revision{}); err != nil {
		t.Fatal(err)
	}
	admit := func(context.Context) error { return nil }
	before, _ := os.ReadFile(store.path)
	replace := store.publishSection
	store.publishSection = func([]byte) error { return errors.New("synthetic failure") }
	if _, _, err := store.ReplaceCredentialChecked(t.Context(), value.ID(), value.Revision(), "", old, credentialref.Parse("env:NEXT"), "local", "retry", admit); err == nil {
		t.Fatal("failed commit accepted")
	}
	after, _ := os.ReadFile(store.path)
	if string(before) != string(after) || len(store.StagedCredentials()) != 0 {
		t.Fatal("failed replacement changed cleanup tracking")
	}
	store.publishSection = replace
	committed, _, err := store.ReplaceCredentialChecked(t.Context(), value.ID(), value.Revision(), "", old, credentialref.Parse("env:NEXT"), "local", "retry", admit)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := openInlineStoreFixture(t, store.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := workspaces.Close(); err != nil {
		t.Fatal(err)
	}
	restartDir := t.TempDir()
	if err := os.Chmod(restartDir, 0700); err != nil {
		t.Fatal(err)
	}
	restartedWorkspaces, err := configstore.OpenOrCreate(filepath.Join(restartDir, "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer restartedWorkspaces.Close()
	lifecycle, err := NewLifecycle(reopened.Store, restartedWorkspaces)
	if err != nil {
		t.Fatal(err)
	}
	retired := 0
	cleanupAt := time.Now().Add(2 * CredentialStageLease)
	if err := lifecycle.ReconcileStagedCredentials(t.Context(), cleanupAt, func(context.Context, connectionidentity.ID, credentialref.Ref) error {
		t.Fatal("retired shared binding")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.DeleteIfUnused(t.Context(), sharedID, shared.Revision()); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.ReconcileStagedCredentials(t.Context(), cleanupAt, func(context.Context, connectionidentity.ID, credentialref.Ref) error {
		return errors.New("synthetic backend unavailable")
	}); err == nil {
		t.Fatal("cleanup failure disappeared")
	}
	if len(reopened.StagedCredentials()) != 1 {
		t.Fatal("lost failed cleanup metadata")
	}
	if err := lifecycle.ReconcileStagedCredentials(t.Context(), cleanupAt, func(_ context.Context, id connectionidentity.ID, ref credentialref.Ref) error {
		if id != value.ID() || ref != old {
			t.Fatal("wrong cleanup authority")
		}
		retired++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if retired != 1 {
		t.Fatalf("old owned binding cleanup calls=%d want=1", retired)
	}
	current, _ := reopened.Get(value.ID())
	if current.Revision() != committed.Revision() || len(reopened.Audits()) != 1 {
		t.Fatal("maintenance changed configuration or audit")
	}
}

func TestEditAndDeleteTrackUnstagedOwnedBindings(t *testing.T) {
	for _, operation := range []string{"edit", "delete"} {
		t.Run(operation, func(t *testing.T) {
			lifecycle, store, _, base, _ := lifecycleFixture(t)
			old := credentialref.Parse("secretfile:connections/" + base.ID().String() + "/cred_" + strings.Repeat("b", 32))
			spec, _ := NewMCPHTTPSpec(base.Spec().(MCPHTTPSpec).Endpoint(), old)
			value, err := base.EditSameIdentity(base.Revision(), base.DisplayName(), spec, time.Now().Add(-time.Second))
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Publish(value, base.Revision()); err != nil {
				t.Fatal(err)
			}
			mutate := func() error {
				if operation == "delete" {
					return lifecycle.DeleteIfUnused(t.Context(), value.ID(), value.Revision())
				}
				nextSpec, _ := NewMCPHTTPSpec(spec.Endpoint(), credentialref.Parse("env:NEXT"))
				next, err := value.EditSameIdentity(value.Revision(), value.DisplayName(), nextSpec, time.Now())
				if err != nil {
					return err
				}
				return store.Publish(next, value.Revision())
			}
			before, _ := os.ReadFile(store.path)
			replace := store.publishSection
			store.publishSection = func([]byte) error { return errors.New("synthetic write failure") }
			if err := mutate(); err == nil {
				t.Fatal("failed mutation accepted")
			}
			after, _ := os.ReadFile(store.path)
			if string(before) != string(after) || len(store.StagedCredentials()) != 0 {
				t.Fatal("failed mutation published cleanup state")
			}
			store.publishSection = replace
			if err := mutate(); err != nil {
				t.Fatal(err)
			}
			reopened, err := openInlineStoreFixture(t, store.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			entries := reopened.StagedCredentials()
			if len(entries) != 1 || entries[0].Reference != old.String() || entries[0].ConnectionID != value.ID().String() {
				t.Fatalf("removed binding not tracked: %+v", entries)
			}
			calls := 0
			if err := lifecycle.ReconcileStagedCredentials(t.Context(), time.Now().Add(2*CredentialStageLease), func(_ context.Context, id connectionidentity.ID, ref credentialref.Ref) error {
				if id != value.ID() || ref != old {
					t.Fatal("wrong cleanup authority")
				}
				calls++
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("cleanup calls=%d", calls)
			}
		})
	}
}
