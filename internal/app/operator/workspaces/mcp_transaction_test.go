package workspaces

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/publications"
	"github.com/swobuforge/swobu/internal/routing"
)

// Owner changes must invalidate consent without publishing any Workspace state.
// A failed Node write must preserve the same bytes and revision, allowing a
// subsequent explicit apply of the still-current review.
func TestMCPMountTransactionFencesOwnersAndFailedPublication(t *testing.T) {
	for _, changed := range []string{"access", "publication", "connection", "write", "authorization"} {
		t.Run(changed, func(t *testing.T) {
			service, store, path := testService(t)
			node, err := nodefile.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			failWrite := false
			if err := store.InstallNodePublisher(func(raw []byte) error {
				if failWrite {
					return errors.New("injected Node write failure")
				}
				return node.PublishRouting(raw)
			}); err != nil {
				t.Fatal(err)
			}
			registry, err := connections.OpenInline(nil, node.PublishConnections, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := connections.NewLifecycle(registry, store); err != nil {
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
			service.connections, service.access, service.publications = registry, access, published
			endpoint, err := connections.ParseEndpoint("https://mcp.example.test/mcp")
			if err != nil {
				t.Fatal(err)
			}
			spec, err := connections.NewMCPHTTPSpec(endpoint, credentialref.Ref{})
			if err != nil {
				t.Fatal(err)
			}
			spec, err = spec.WithOperatorDeclarations("Proof account", "Read documents")
			if err != nil {
				t.Fatal(err)
			}
			id, err := connectionidentity.Parse("cn_docs")
			if err != nil {
				t.Fatal(err)
			}
			connection, err := connections.New(id, "Docs", spec, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := registry.Publish(connection, connections.Revision{}); err != nil {
				t.Fatal(err)
			}
			admit := func(context.Context) error { return nil }
			if _, _, err := service.CreateWorkspaceChecked(t.Context(), CreateWorkspace{Slug: "research"}, "operator", "create", admit); err != nil {
				t.Fatal(err)
			}
			workspace, err := service.GetWorkspace(t.Context(), "research")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.SetServiceChecked(t.Context(), "research", "mcp", true, workspace.ManagementETag, admit); err != nil {
				t.Fatal(err)
			}
			draft := MCPMountDraft{Workspace: "research", Alias: "docs", ConnectionID: id.String(), SelectedTools: []string{"read"}}
			preview, err := service.PreviewMCPMount(t.Context(), draft)
			if err != nil {
				t.Fatal(err)
			}
			review := MCPMountReviewRequest{WorkspaceETag: preview.Revision, Digest: preview.Digest, ConnectionRevision: preview.ConnectionRevision, AccessRevision: preview.AccessRevision, PublicationRevision: preview.PublicationRevision}
			slug, _ := routing.ParseWorkspaceSlug("research")
			switch changed {
			case "access":
				if _, _, err := access.CreatePrincipalChecked(t.Context(), "New principal", "new-principal", func() error { return nil }); err != nil {
					t.Fatal(err)
				}
			case "publication":
				if err := published.Set(slug, true, published.Revision(), func() error { return nil }); err != nil {
					t.Fatal(err)
				}
			case "connection":
				edited, err := connection.EditSameIdentity(connection.Revision(), "Changed account declaration", spec, time.Now().Add(time.Second))
				if err != nil {
					t.Fatal(err)
				}
				if err := registry.Publish(edited, connection.Revision()); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			current, err := store.GetWorkspace(t.Context(), slug)
			if err != nil {
				t.Fatal(err)
			}
			failWrite = changed == "write"
			authorize := admit
			if changed == "authorization" {
				authorize = func(context.Context) error { return errors.New("revoked authorization") }
			}
			if _, err := service.ApplyMCPMountChecked(t.Context(), draft, review, authorize); err == nil {
				t.Fatal("changed owner or failed publication accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			still, err := store.GetWorkspace(t.Context(), slug)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) || still.ManagementRevision() != current.ManagementRevision() || len(still.MCPMounts()) != 0 {
				t.Fatal("failed mount changed bytes, revision or memory")
			}
			// Re-review current owners and recover using an explicit new decision.
			failWrite = false
			fresh, err := service.PreviewMCPMount(t.Context(), draft)
			if err != nil {
				t.Fatal(err)
			}
			freshReview := MCPMountReviewRequest{WorkspaceETag: fresh.Revision, Digest: fresh.Digest, ConnectionRevision: fresh.ConnectionRevision, AccessRevision: fresh.AccessRevision, PublicationRevision: fresh.PublicationRevision}
			if _, err := service.ApplyMCPMountChecked(t.Context(), draft, freshReview, admit); err != nil {
				t.Fatal(err)
			}
		})
	}
}
