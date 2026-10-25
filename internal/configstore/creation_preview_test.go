package configstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestCreationPreviewHasNoPublicationAuthorityOrReservation(t *testing.T) {
	store, path := openTestStore(t)
	draft := creationDraft(t, "Research")
	before, _ := os.ReadFile(path)
	admit := func(context.Context) error { return nil }
	if err := store.PreviewWorkspaceCreation(t.Context(), draft, admit); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || len(store.creations) != 0 || len(store.Config().Workspaces()) != 0 {
		t.Fatal("preview persisted or minted state")
	}
	if _, _, err := store.CreateWorkspaceChecked(t.Context(), draft, "local", "competing", admit); err != nil {
		t.Fatal("preview reserved slug", err)
	}
	committed, _ := os.ReadFile(path)
	if err := store.PreviewWorkspaceCreation(t.Context(), draft, admit); !errors.Is(err, routing.ErrConflict) {
		t.Fatal("preview ignored competing creation", err)
	}
	if _, _, err := store.CreateWorkspaceChecked(t.Context(), draft, "local", "reviewed", admit); !errors.Is(err, routing.ErrConflict) {
		t.Fatal("apply trusted stale preview", err)
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(committed, after) || len(store.creations) != 1 {
		t.Fatal("conflict published")
	}
}

func TestCreationPreviewAdmissionCancellationSchemaAndClosedState(t *testing.T) {
	store, path := openTestStore(t)
	draft := creationDraft(t, "Research")
	before, _ := os.ReadFile(path)
	denied := errors.New("revoked")
	if err := store.PreviewWorkspaceCreation(t.Context(), draft, func(context.Context) error { return denied }); !errors.Is(err, denied) {
		t.Fatal("preview admission lost", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	if err := store.PreviewWorkspaceCreation(ctx, draft, func(context.Context) error { cancel(); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled preview accepted", err)
	}
	composed, err := routing.RestoreWorkspaceWithSearchConnection(draft.Slug(), nil, nil, nil, "search")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PreviewWorkspaceCreation(t.Context(), composed, func(context.Context) error { return nil }); err != nil {
		t.Fatal("current preview rejected composed draft")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || len(store.creations) != 0 {
		t.Fatal("rejected preview published")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.PreviewWorkspaceCreation(t.Context(), draft, func(context.Context) error { return nil }); !errors.Is(err, ErrStoreClosed) {
		t.Fatal("closed preview accepted", err)
	}
}
