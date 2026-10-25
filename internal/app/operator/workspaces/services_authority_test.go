package workspaces

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/publications"
	"github.com/swobuforge/swobu/internal/routing"
)

// A broad grant predating Service activation must not silently acquire a new
// capability. The reviewed effect is fenced by current Access and publication
// authority, not only the Workspace routing ETag.
func TestServiceActivationMustReviewEffectiveAuthorityExpansion(t *testing.T) {
	service, store, path := testService(t)
	node, err := nodefile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InstallNodePublisher(node.PublishRouting); err != nil {
		t.Fatal(err)
	}
	access, err := accessstate.OpenInline(nil, node.PublishPrincipals)
	if err != nil {
		t.Fatal(err)
	}
	published, err := publications.OpenInline(nil, node.PublishPublications)
	if err != nil {
		t.Fatal(err)
	}
	service.access, service.publications = access, published
	admit := func(context.Context) error { return nil }
	initial, _, err := service.CreateWorkspaceChecked(t.Context(), CreateWorkspace{Slug: "research"}, "operator", "create", admit)
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	principal, _, err := access.Create("automation", slug)
	if err != nil {
		t.Fatal(err)
	}
	states, err := service.Services(t.Context(), "research")
	if err != nil {
		t.Fatal(err)
	}
	review := states[0].ActivationReview
	if review == nil || !review.RequiresConfirmation || len(review.AffectedPrincipals) != 1 || review.AffectedPrincipals[0].ID != principal.ID.String() {
		t.Fatalf("broad grant not presented: %+v", review)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetServiceChecked(t.Context(), "research", "models", true, initial.ManagementETag, admit); !errors.Is(err, ErrServiceReviewRequired) {
		t.Fatalf("silent expansion = %v", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("unreviewed enable wrote Node state")
	}
	if _, err := service.SetServiceReviewed(t.Context(), "research", "models", true, initial.ManagementETag, "wrong", admit); !errors.Is(err, ErrServiceReviewChanged) {
		t.Fatalf("forged review = %v", err)
	}
	enabled, err := service.SetServiceReviewed(t.Context(), "research", "models", true, initial.ManagementETag, review.Digest, admit)
	if err != nil || len(enabled.EnabledServices) != 1 {
		t.Fatalf("reviewed activation: %+v %v", enabled, err)
	}
}

func TestServiceActivationStaleReviewRejectsOtherAuthorityChanges(t *testing.T) {
	for _, changed := range []string{"principal", "publication"} {
		t.Run(changed, func(t *testing.T) {
			service, store, path := testService(t)
			node, err := nodefile.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.InstallNodePublisher(node.PublishRouting); err != nil {
				t.Fatal(err)
			}
			access, err := accessstate.OpenInline(nil, node.PublishPrincipals)
			if err != nil {
				t.Fatal(err)
			}
			published, err := publications.OpenInline(nil, node.PublishPublications)
			if err != nil {
				t.Fatal(err)
			}
			service.access, service.publications = access, published
			admit := func(context.Context) error { return nil }
			current, _, err := service.CreateWorkspaceChecked(t.Context(), CreateWorkspace{Slug: "research"}, "operator", "create", admit)
			if err != nil {
				t.Fatal(err)
			}
			slug, _ := routing.ParseWorkspaceSlug("research")
			if _, _, err := access.Create("initial", slug); err != nil {
				t.Fatal(err)
			}
			states, err := service.Services(t.Context(), "research")
			if err != nil {
				t.Fatal(err)
			}
			review := states[0].ActivationReview
			if review == nil || !review.RequiresConfirmation {
				t.Fatal("missing preview")
			}
			switch changed {
			case "principal":
				if _, _, err := access.Create("new recipient", slug); err != nil {
					t.Fatal(err)
				}
			case "publication":
				if err := published.Set(slug, true, published.Revision(), func() error { return nil }); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.SetServiceReviewed(t.Context(), "research", "models", true, current.ManagementETag, review.Digest, admit); !errors.Is(err, ErrServiceReviewChanged) {
				t.Fatalf("%s changed but old review accepted: %v", changed, err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("stale review mutated Node bytes")
			}
			fresh, err := service.Services(t.Context(), "research")
			if err != nil {
				t.Fatal(err)
			}
			if fresh[0].ActivationReview.Digest == review.Digest {
				t.Fatal("changed owner retained review digest")
			}
			if _, err := service.SetServiceReviewed(t.Context(), "research", "models", true, current.ManagementETag, fresh[0].ActivationReview.Digest, admit); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestServiceActivationNoAuthorityImpactKeepsSimpleCreation(t *testing.T) {
	service, _, _ := testService(t)
	admit := func(context.Context) error { return nil }
	current, _, err := service.CreateWorkspaceChecked(t.Context(), CreateWorkspace{Slug: "empty"}, "operator", "create", admit)
	if err != nil {
		t.Fatal(err)
	}
	states, err := service.Services(t.Context(), "empty")
	if err != nil {
		t.Fatal(err)
	}
	if states[0].ActivationReview == nil || states[0].ActivationReview.RequiresConfirmation {
		t.Fatalf("empty review: %+v", states[0].ActivationReview)
	}
	if _, err := service.SetServiceChecked(t.Context(), "empty", "models", true, current.ManagementETag, admit); err != nil {
		t.Fatal(err)
	}
}

func TestServiceActivationPublishedWorkspaceRequiresReviewWithoutGrants(t *testing.T) {
	service, store, path := testService(t)
	node, err := nodefile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InstallNodePublisher(node.PublishRouting); err != nil {
		t.Fatal(err)
	}
	published, err := publications.OpenInline(nil, node.PublishPublications)
	if err != nil {
		t.Fatal(err)
	}
	service.publications = published
	admit := func(context.Context) error { return nil }
	current, _, err := service.CreateWorkspaceChecked(t.Context(), CreateWorkspace{Slug: "public"}, "operator", "create", admit)
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("public")
	if err := published.Set(slug, true, published.Revision(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	states, err := service.Services(t.Context(), "public")
	if err != nil {
		t.Fatal(err)
	}
	review := states[0].ActivationReview
	if review == nil || !review.Published || !review.RequiresConfirmation || len(review.AffectedPrincipals) != 0 {
		t.Fatalf("publication-only impact was not reviewed: %+v", review)
	}
	if _, err := service.SetServiceChecked(t.Context(), "public", "models", true, current.ManagementETag, admit); !errors.Is(err, ErrServiceReviewRequired) {
		t.Fatalf("publication-only activation unexpectedly permitted: %v", err)
	}
	if _, err := service.SetServiceReviewed(t.Context(), "public", "models", true, current.ManagementETag, review.Digest, admit); err != nil {
		t.Fatal(err)
	}
}

func TestServiceActivationDigestCannotAuthorizeAnotherService(t *testing.T) {
	service, store, path := testService(t)
	node, err := nodefile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InstallNodePublisher(node.PublishRouting); err != nil {
		t.Fatal(err)
	}
	access, err := accessstate.OpenInline(nil, node.PublishPrincipals)
	if err != nil {
		t.Fatal(err)
	}
	service.access = access
	admit := func(context.Context) error { return nil }
	current, _, err := service.CreateWorkspaceChecked(t.Context(), CreateWorkspace{Slug: "mixed"}, "operator", "create", admit)
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("mixed")
	if _, _, err := access.Create("shared", slug); err != nil {
		t.Fatal(err)
	}
	states, err := service.Services(t.Context(), "mixed")
	if err != nil {
		t.Fatal(err)
	}
	if states[0].ActivationReview.Digest == states[1].ActivationReview.Digest {
		t.Fatal("cross-Service review digest collision")
	}
	if _, err := service.SetServiceReviewed(t.Context(), "mixed", "mcp", true, current.ManagementETag, states[0].ActivationReview.Digest, admit); !errors.Is(err, ErrServiceReviewChanged) {
		t.Fatalf("Models authority review permitted MCP: %v", err)
	}
}

// Review and publication must be one serialization interval. A concurrent
// authority writer cannot change the reviewed audience between admission and
// the durable Service commit.
func TestServiceActivationSerializesConcurrentAuthorityWriters(t *testing.T) {
	for _, owner := range []string{"access", "publication"} {
		t.Run(owner, func(t *testing.T) {
			service, store, path := testService(t)
			node, err := nodefile.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.InstallNodePublisher(node.PublishRouting); err != nil {
				t.Fatal(err)
			}
			access, err := accessstate.OpenInline(nil, node.PublishPrincipals)
			if err != nil {
				t.Fatal(err)
			}
			published, err := publications.OpenInline(nil, node.PublishPublications)
			if err != nil {
				t.Fatal(err)
			}
			service.access, service.publications = access, published
			current, _, err := service.CreateWorkspaceChecked(t.Context(), CreateWorkspace{Slug: "serialized"}, "operator", "create", func(context.Context) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			slug, _ := routing.ParseWorkspaceSlug("serialized")
			if _, _, err := access.Create("initial", slug); err != nil {
				t.Fatal(err)
			}
			states, err := service.Services(t.Context(), "serialized")
			if err != nil {
				t.Fatal(err)
			}
			admitted, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			committed := make(chan error, 1)
			go func() {
				_, err := service.SetServiceReviewed(t.Context(), "serialized", "models", true, current.ManagementETag, states[0].ActivationReview.Digest, func(context.Context) error {
					close(admitted)
					<-release
					return nil
				})
				committed <- err
			}()
			select {
			case <-admitted:
			case err := <-committed:
				t.Fatalf("activation did not reach admission: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("activation stalled before admission")
			}
			started, written := make(chan struct{}), make(chan error, 1)
			go func() {
				close(started)
				if owner == "access" {
					_, _, err := access.Create("late audience", slug)
					written <- err
				} else {
					written <- published.Set(slug, true, states[0].ActivationReview.PublicationRevision, func() error { return nil })
				}
			}()
			<-started
			select {
			case err := <-written:
				t.Fatalf("%s writer escaped activation interval: %v", owner, err)
			case <-time.After(50 * time.Millisecond):
			}
			unblock()
			for _, done := range []<-chan error{committed, written} {
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("serialized operation did not finish")
				}
			}
			final, err := service.GetWorkspace(t.Context(), "serialized")
			if err != nil || len(final.EnabledServices) != 1 {
				t.Fatalf("activation was not durable: %+v %v", final, err)
			}
		})
	}
}
