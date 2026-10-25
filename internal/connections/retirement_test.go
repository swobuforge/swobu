package connections

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestRetirementBlocksSharedMaterialThenDeniesRebindAfterRestart(t *testing.T) {
	lifecycle, store, _, base, _ := lifecycleFixture(t)
	owned := credentialref.Parse("secretfile:connections/" + base.ID().String() + "/cred_" + strings.Repeat("a", 32))
	spec, _ := NewMCPHTTPSpec(base.Spec().(MCPHTTPSpec).Endpoint(), owned)
	first, err := base.EditSameIdentity(base.Revision(), base.DisplayName(), spec, base.UpdatedAt().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(first, base.Revision()); err != nil {
		t.Fatal(err)
	}
	secondID, _ := connectionidentity.New()
	second, err := New(secondID, "Shared", spec, first.UpdatedAt())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(second, Revision{}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	retire := func(context.Context, connectionidentity.ID, credentialref.Ref) error { calls++; return nil }
	if err := lifecycle.RetireCredentialIfUnused(t.Context(), base.ID(), owned, retire); !errors.Is(err, ErrCredentialInUse) {
		t.Fatalf("live ref: %v", err)
	}
	// Remove both configurations, retaining the ownership identity in the locator.
	if err := lifecycle.DeleteIfUnused(t.Context(), first.ID(), first.Revision()); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.RetireCredentialIfUnused(t.Context(), base.ID(), owned, retire); !errors.Is(err, ErrCredentialInUse) {
		t.Fatalf("shared ref: %v", err)
	}
	if err := lifecycle.DeleteIfUnused(t.Context(), second.ID(), second.Revision()); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.RetireCredentialIfUnused(t.Context(), base.ID(), owned, retire); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("retired while referenced")
	}
	reopened, err := openInlineStoreFixture(t, store.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	newID, _ := connectionidentity.New()
	candidate, _ := New(newID, "Rebind", spec, first.UpdatedAt())
	if err := reopened.Publish(candidate, Revision{}); !errors.Is(err, ErrCredentialRetired) {
		t.Fatalf("retired slot rebound: %v", err)
	}
	if len(reopened.List()) != 0 {
		t.Fatal("failed rebind changed state")
	}
}

func TestRetirementMarkerMustCommitBeforeStorageAndRetriesFailure(t *testing.T) {
	lifecycle, store, _, base, _ := lifecycleFixture(t)
	owned := credentialref.Parse("secret:connections/" + base.ID().String() + "/cred_" + strings.Repeat("b", 32))
	calls := 0
	retire := func(context.Context, connectionidentity.ID, credentialref.Ref) error {
		calls++
		return errors.New("storage unavailable")
	}
	before, _ := os.ReadFile(store.path)
	replace := store.publishSection
	store.publishSection = func([]byte) error { return errors.New("write failure") }
	if err := lifecycle.RetireCredentialIfUnused(t.Context(), base.ID(), owned, retire); err == nil {
		t.Fatal("failed marker accepted")
	}
	after, _ := os.ReadFile(store.path)
	if calls != 0 || len(store.retired) != 0 || string(before) != string(after) {
		t.Fatal("storage ran before durable marker")
	}
	store.publishSection = replace
	if err := lifecycle.RetireCredentialIfUnused(t.Context(), base.ID(), owned, retire); err == nil {
		t.Fatal("storage failure hidden")
	}
	if len(store.retired) != 1 || calls != 1 {
		t.Fatal("retry marker lost")
	}
	if err := lifecycle.RetireCredentialIfUnused(t.Context(), base.ID(), owned, func(context.Context, connectionidentity.ID, credentialref.Ref) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("failed storage not retried")
	}
	if err := lifecycle.RetireCredentialIfUnused(t.Context(), base.ID(), credentialref.Parse("env:EXTERNAL"), retire); err == nil {
		t.Fatal("external material retired")
	}
}

func TestRetirementExcludesCurrentModelRegistryBinding(t *testing.T) {
	lifecycle, store, workspaces, base, _ := lifecycleFixture(t)
	id, _ := connectionidentity.Parse("cn_model")
	ref := credentialref.Parse("secretfile:connections/" + id.String() + "/cred_" + strings.Repeat("c", 32))
	upstream, err := routing.NewStandardConnection("openai", "", ref.String())
	if err != nil {
		t.Fatal(err)
	}
	spec, err := NewModelSpec(upstream)
	if err != nil {
		t.Fatal(err)
	}
	modelConnection, err := New(id, "model", spec, base.CreatedAt())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(modelConnection, Revision{}); err != nil {
		t.Fatal(err)
	}
	target, err := routing.FinalizeTarget(routing.TargetDraft{ID: "target", Model: "model", Protocol: "responses", ConnectionID: id.String()}, routing.TargetConstructionFacts{})
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	route, _ := routing.ParseRouteName("chat")
	attached, err := (routing.Config{}).CreateWorkspace(routing.WorkspaceSeed{Slug: slug, Route: route, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.Update(t.Context(), func(routing.Config) (routing.Config, error) { return attached, nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	retire := func(context.Context, connectionidentity.ID, credentialref.Ref) error { calls++; return nil }
	if err := lifecycle.RetireCredentialIfUnused(t.Context(), id, ref, retire); !errors.Is(err, ErrCredentialInUse) {
		t.Fatal("live model binding missed", err)
	}
	if _, err := workspaces.Update(t.Context(), func(routing.Config) (routing.Config, error) { return routing.Config{}, nil }); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.DeleteIfUnused(t.Context(), id, modelConnection.Revision()); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.RetireCredentialIfUnused(t.Context(), id, ref, retire); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("retirement effect count changed")
	}
	if err := store.Publish(modelConnection, Revision{}); !errors.Is(err, ErrCredentialRetired) {
		t.Fatal("registry rebound retired locator", err)
	}
	if _, err := workspaces.Update(t.Context(), func(routing.Config) (routing.Config, error) { return attached, nil }); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing model reference admitted", err)
	}
}

func TestRetirementSerializesConcurrentConnectionRebind(t *testing.T) {
	lifecycle, store, _, base, _ := lifecycleFixture(t)
	ref := credentialref.Parse("secret:connections/" + base.ID().String() + "/cred_" + strings.Repeat("d", 32))
	spec, _ := NewMCPHTTPSpec(base.Spec().(MCPHTTPSpec).Endpoint(), ref)
	id, _ := connectionidentity.New()
	candidate, _ := New(id, "Rebind", spec, base.CreatedAt())
	entered := make(chan struct{})
	release := make(chan struct{})
	retired := make(chan error, 1)
	go func() {
		retired <- lifecycle.RetireCredentialIfUnused(t.Context(), base.ID(), ref, func(context.Context, connectionidentity.ID, credentialref.Ref) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	result := make(chan error, 1)
	go func() { result <- store.Publish(candidate, Revision{}) }()
	close(release)
	if err := <-retired; err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrCredentialRetired) {
		t.Fatalf("racing rebind: %v", err)
	}
}

func TestRetirementRejectsCorruptMarkerOnRestart(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":1,"connections":[],"retired_credentials":{"env:EXTERNAL":"cn_a"}}`,
		`{"schema_version":1,"connections":[],"retired_credentials":{"secret:connections/cn_b/cred_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa":"cn_a"}}`,
	} {
		if _, err := OpenInline([]byte(raw), func([]byte) error { return nil }, nil); err == nil {
			t.Fatal("corrupt retirement marker admitted")
		}
	}
}
