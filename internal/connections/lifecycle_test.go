package connections

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
)

func lifecycleFixture(t *testing.T) (*Lifecycle, *inlineStoreFixture, *configstore.Store, Connection, routing.Config) {
	t.Helper()
	registry, err := openInlineStoreFixture(t, filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	connection := fixtureConnection(t)
	if err := registry.Publish(connection, Revision{}); err != nil {
		t.Fatal(err)
	}
	workspaceDir := t.TempDir()
	if err := os.Chmod(workspaceDir, 0700); err != nil {
		t.Fatal(err)
	}
	workspaces, err := configstore.OpenOrCreate(filepath.Join(workspaceDir, "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaces.Close() })
	lifecycle, err := NewLifecycle(registry.Store, workspaces)
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	workspace, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{"docs": {ConnectionID: connection.ID().String()}})
	if err != nil {
		t.Fatal(err)
	}
	config, err := routing.NewConfig([]routing.Workspace{workspace})
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle, registry, workspaces, connection, config
}

func TestLifecycleDeleteBlocksExactReferencesAndLaterReattachment(t *testing.T) {
	lifecycle, registry, workspaces, connection, attached := lifecycleFixture(t)
	ctx := context.Background()
	if _, err := workspaces.Update(ctx, func(routing.Config) (routing.Config, error) { return attached, nil }); err != nil {
		t.Fatal(err)
	}
	err := lifecycle.DeleteIfUnused(ctx, connection.ID(), connection.Revision())
	var inUse *InUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("delete referenced: %v", err)
	}
	consumers := inUse.Consumers()
	if len(consumers) != 1 || consumers[0] != (Consumer{Workspace: "dev", Kind: MCP, Mount: "docs"}) {
		t.Fatalf("consumers: %+v", consumers)
	}
	consumers[0].Workspace = "changed"
	if inUse.Consumers()[0].Workspace != "dev" {
		t.Fatal("mutable error consumers")
	}
	if _, err := workspaces.Update(ctx, func(routing.Config) (routing.Config, error) { return routing.Config{}, nil }); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.DeleteIfUnused(ctx, connection.ID(), Revision{}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("missing consent: %v", err)
	}
	if err := lifecycle.DeleteIfUnused(ctx, connection.ID(), connection.Revision()); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Get(connection.ID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted readback: %v", err)
	}
	reopened, err := openInlineStoreFixture(t, registry.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.List()) != 0 {
		t.Fatal("delete was not durable")
	}
	if _, err := workspaces.Update(ctx, func(routing.Config) (routing.Config, error) { return attached, nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reattached deleted identity: %v", err)
	}
	if workspaces.Config().WorkspaceCount() != 0 {
		t.Fatal("failed admission published routing")
	}
}

func TestLifecycleConcurrentAttachAndDeleteCannotDangle(t *testing.T) {
	for range 20 {
		lifecycle, registry, workspaces, connection, attached := lifecycleFixture(t)
		start := make(chan struct{})
		var group sync.WaitGroup
		var deleteErr, attachErr error
		group.Add(2)
		go func() {
			defer group.Done()
			<-start
			deleteErr = lifecycle.DeleteIfUnused(context.Background(), connection.ID(), connection.Revision())
		}()
		go func() {
			defer group.Done()
			<-start
			_, attachErr = workspaces.Update(context.Background(), func(routing.Config) (routing.Config, error) { return attached, nil })
		}()
		close(start)
		group.Wait()
		_, existsErr := registry.Get(connection.ID())
		if deleteErr == nil {
			if !errors.Is(attachErr, ErrNotFound) || !errors.Is(existsErr, ErrNotFound) || workspaces.Config().WorkspaceCount() != 0 {
				t.Fatalf("delete won with dangling attachment: delete=%v attach=%v", deleteErr, attachErr)
			}
		} else {
			var inUse *InUseError
			if !errors.As(deleteErr, &inUse) || attachErr != nil || existsErr != nil || workspaces.Config().WorkspaceCount() != 1 {
				t.Fatalf("attach won inconsistently: delete=%v attach=%v", deleteErr, attachErr)
			}
		}
	}
}

func TestLifecycleFailedDeletePreservesDiskAndMemory(t *testing.T) {
	lifecycle, registry, _, connection, _ := lifecycleFixture(t)
	before, err := os.ReadFile(registry.path)
	if err != nil {
		t.Fatal(err)
	}
	registry.publishSection = func([]byte) error { return errors.New("injected failure") }
	if err := lifecycle.DeleteIfUnused(context.Background(), connection.ID(), connection.Revision()); err == nil {
		t.Fatal("accepted failed deletion")
	}
	after, err := os.ReadFile(registry.path)
	if err != nil || string(before) != string(after) {
		t.Fatal("failed delete changed durable state")
	}
	if _, err := registry.Get(connection.ID()); err != nil {
		t.Fatal("failed delete changed memory")
	}
}

func TestLifecycleRejectsStaleDeleteAndPreservesHealthOnWriteFailure(t *testing.T) {
	lifecycle, registry, _, connection, _ := lifecycleFixture(t)
	edited, err := connection.EditSameIdentity(connection.Revision(), "Renamed", connection.Spec(), connection.UpdatedAt().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(edited, connection.Revision()); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.DeleteIfUnused(context.Background(), connection.ID(), connection.Revision()); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	check, err := NewCheck(edited, edited.UpdatedAt(), Reachable, AuthenticationUnknown, nil, CheckOK)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Observe(check); err != nil {
		t.Fatal(err)
	}
	replace := registry.publishSection
	registry.publishSection = func([]byte) error { return errors.New("injected failure") }
	if err := lifecycle.DeleteIfUnused(context.Background(), edited.ID(), edited.Revision()); err == nil {
		t.Fatal("accepted failed deletion")
	}
	if _, ok := registry.LatestCheck(edited.ID()); !ok {
		t.Fatal("failed delete erased health")
	}
	registry.publishSection = replace
	if err := lifecycle.DeleteIfUnused(context.Background(), edited.ID(), edited.Revision()); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.checks[edited.ID()]; ok {
		t.Fatal("successful delete retained health")
	}
}

func TestLifecycleAdmissionRejectsWrongKindAndMissingMigratedIdentity(t *testing.T) {
	missingID, err := connectionidentity.Parse("cn_missing")
	if err != nil {
		t.Fatal(err)
	}
	_, registry, _, connection, attached := lifecycleFixture(t)
	for _, test := range []struct {
		name string
		id   connectionidentity.ID
		want error
	}{
		{"wrong-kind", connection.ID(), ErrReferenceKind},
		{"missing", missingID, ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			workspaces, err := configstore.OpenOrCreate(filepath.Join(dir, "routing.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			defer workspaces.Close()
			candidate := attached
			if test.want == ErrReferenceKind {
				// The MCP source deliberately points at a validated Models account.
				provider, _ := routing.ParseProvider("openai", func(string) bool { return true })
				upstream, err := routing.NewStandardConnection(provider, "", "env:ACCOUNT")
				if err != nil {
					t.Fatal(err)
				}
				spec, err := NewModelSpec(upstream)
				if err != nil {
					t.Fatal(err)
				}
				id, _ := connectionidentity.New()
				model, err := New(id, "Account", spec, connection.CreatedAt())
				if err != nil {
					t.Fatal(err)
				}
				if err := registry.Publish(model, Revision{}); err != nil {
					t.Fatal(err)
				}
				candidate, err = candidate.SetMCPMount(candidate.Workspaces()[0].Slug(), "docs", routing.MCPMount{ConnectionID: id.String()})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				candidate, err = candidate.SetMCPMount(candidate.Workspaces()[0].Slug(), "docs", routing.MCPMount{ConnectionID: test.id.String()})
				if err != nil {
					t.Fatal(err)
				}
			}
			// Install against existing current references, not merely a future edit.
			if _, err := workspaces.Update(context.Background(), func(routing.Config) (routing.Config, error) { return candidate, nil }); err != nil {
				t.Fatal(err)
			}
			if _, err := NewLifecycle(registry.Store, workspaces); !errors.Is(err, test.want) {
				t.Fatalf("invalid current reference admitted: %v", err)
			}
		})
	}
}

// A reviewer can observe revision 1, then a legitimate Connection editor can
// commit revision 2 before Workspace publication. The original approved mount
// must not publish. This forces that interleaving in the existing guard seam,
// so the regression does not depend on timing or scheduler luck.
func TestMCPMountReviewedConnectionRevisionCheckedAtPublication(t *testing.T) {
	_, registry, workspaces, connection, attached := lifecycleFixture(t)
	ctx := context.Background()
	if _, err := workspaces.Update(ctx, func(routing.Config) (routing.Config, error) { return attached, nil }); err != nil {
		t.Fatal(err)
	}
	before := workspaces.Config()
	workspace := before.Workspaces()[0]
	etag := fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision())
	changed, err := connection.EditSameIdentity(connection.Revision(), "Edited after review", connection.Spec(), connection.UpdatedAt().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	didEdit := false
	_, err = workspaces.ApplyMCPMountChecked(ctx, workspace.Slug(), "another", routing.MCPMount{
		ConnectionID: connection.ID().String(), AllowedTools: []string{"list_resources"},
	}, etag, connection.Revision().String(), func(context.Context) error { return nil },
		func(_ routing.Workspace, publish func() error) error {
			if err := registry.Publish(changed, connection.Revision()); err != nil {
				return err
			}
			didEdit = true
			return publish()
		})
	if !didEdit || !errors.Is(err, configstore.ErrMCPMountReviewConflict) {
		t.Fatalf("stale MCP review: edit=%v error=%v", didEdit, err)
	}
	after := workspaces.Config()
	if len(after.Workspaces()) != len(before.Workspaces()) || after.Workspaces()[0].ManagementRevision() != workspace.ManagementRevision() {
		t.Fatal("failed stale review advanced Workspace revision")
	}
	if _, exists := after.Workspaces()[0].MCPMounts()["another"]; exists {
		t.Fatal("stale mount was published")
	}
	if live, err := registry.Get(connection.ID()); err != nil || live.Revision() != changed.Revision() {
		t.Fatalf("legitimate concurrent Connection edit was lost: %v %v", live.Revision(), err)
	}
	_, err = workspaces.ApplyMCPMountChecked(ctx, workspace.Slug(), "another", routing.MCPMount{
		ConnectionID: connection.ID().String(), AllowedTools: []string{"list_resources"},
	}, etag, changed.Revision().String(), func(context.Context) error { return nil },
		func(_ routing.Workspace, publish func() error) error { return publish() })
	if err != nil {
		t.Fatalf("rejected review did not poison publication: %v", err)
	}
}

// Any routing publication must keep its typed Connection references stable
// until the atomic routing document writer returns. Editing after it returns
// remains allowed: this serializes only the real conflicting critical section.
func TestConnectionEditWaitsForRoutingCommit(t *testing.T) {
	for _, operation := range []string{"edit", "credential replacement"} {
		t.Run(operation, func(t *testing.T) {
			_, registry, workspaces, connection, attached := lifecycleFixture(t)
			entered := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			if err := workspaces.InstallNodePublisher(func(raw []byte) error {
				close(entered)
				<-release
				return nil // publication gate; the test checks locking, not the storage adapter
			}); err != nil {
				t.Fatal(err)
			}
			routeDone := make(chan error, 1)
			go func() {
				_, err := workspaces.Update(context.Background(), func(routing.Config) (routing.Config, error) { return attached, nil })
				routeDone <- err
			}()
			select {
			case <-entered:
			case err := <-routeDone:
				t.Fatalf("routing never entered commit: %v", err)
			case <-time.After(2 * time.Second):
				t.Fatal("routing publisher was never invoked")
			}
			edited, err := connection.EditSameIdentity(connection.Revision(), "Edited after publish", connection.Spec(), connection.UpdatedAt().Add(time.Second))
			if err != nil {
				t.Fatal(err)
			}
			admitted := make(chan struct{})
			editDone := make(chan error, 1)
			go func() {
				admit := func(context.Context) error {
					close(admitted)
					return nil
				}
				if operation == "credential replacement" {
					_, _, err := registry.ReplaceCredentialChecked(context.Background(), connection.ID(), connection.Revision(), "", credentialref.Parse("env:MCP_TOKEN"), credentialref.Parse("env:REPLACEMENT_TOKEN"), "local", "rotation", admit)
					editDone <- err
					return
				}
				editDone <- registry.PublishChecked(context.Background(), edited, connection.Revision(), "local", admit)
			}()
			select {
			case <-admitted:
				unblock()
				t.Fatal("Connection edit entered its publication lock during routing commit")
			case <-time.After(75 * time.Millisecond):
			}
			unblock()
			if err := <-routeDone; err != nil {
				t.Fatalf("routing publication: %v", err)
			}
			if err := <-editDone; err != nil {
				t.Fatalf("Connection edit after routing: %v", err)
			}
			select {
			case <-admitted:
			default:
				t.Fatal("Connection editor was never admitted after routing commit")
			}
		})
	}
}

func TestMCPMountUsesWorkspaceRevisionNotReplayReceipt(t *testing.T) {
	_, registry, workspaces, connection, attached := lifecycleFixture(t)
	ctx := context.Background()
	if _, err := workspaces.Update(ctx, func(routing.Config) (routing.Config, error) { return attached, nil }); err != nil {
		t.Fatal(err)
	}
	current := workspaces.Config().Workspaces()[0]
	etag := fmt.Sprintf("%s-%d", current.ManagementIncarnation(), current.ManagementRevision())
	mount := routing.MCPMount{ConnectionID: connection.ID().String(), AllowedTools: []string{"list_resources"}}
	apply := func() (routing.Workspace, error) {
		return workspaces.ApplyMCPMountChecked(ctx, current.Slug(), "another", mount, etag, connection.Revision().String(), func(context.Context) error { return nil }, func(_ routing.Workspace, publish func() error) error { return publish() })
	}
	first, err := apply()
	if err != nil || first.MCPMounts()["another"].ConnectionID != connection.ID().String() {
		t.Fatalf("mount not committed: %v", err)
	}
	if _, err := apply(); !errors.Is(err, configstore.ErrStaleRevision) {
		t.Fatalf("stale apply must conflict rather than replay: %v", err)
	}
	if value, err := registry.Get(connection.ID()); err != nil || value.Revision() != connection.Revision() {
		t.Fatal("mount mutation changed independent Connection")
	}
}

func TestDeletedReviewedConnectionReturnsReviewConflict(t *testing.T) {
	lifecycle, _, _, connection, candidate := lifecycleFixture(t)
	// This Connection is not yet mounted, so deletion between preview and apply
	// is legitimate. The candidate must report stale review, not a fresh lookup.
	if err := lifecycle.DeleteIfUnused(t.Context(), connection.ID(), connection.Revision()); err != nil {
		t.Fatal(err)
	}
	committed := false
	err := lifecycle.validateReferences(candidate, &configstore.ReviewedConnection{ID: connection.ID().String(), Revision: connection.Revision().String()}, func() error { committed = true; return nil })
	if !errors.Is(err, configstore.ErrMCPMountReviewConflict) || committed {
		t.Fatalf("deleted review: committed=%v err=%v", committed, err)
	}
}
