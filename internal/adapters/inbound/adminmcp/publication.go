package adminmcp

import (
	"context"

	publicationapp "github.com/swobuforge/swobu/internal/app/operator/publication"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/routing"
)

func PublicationTools(service publicationapp.Service) []mcp.AdminTool {
	type workspace struct {
		Workspace string `json:"workspace"`
	}
	return []mcp.AdminTool{
		typedTool("workspace_publication_get", "Read Workspace relay reachability and its original publication revision. Publication is separate from grants and Service enablement.", objectSchema(`"workspace":{"type":"string"}`, "workspace"), func(ctx context.Context, a authority, input workspace) (any, error) {
			slug, err := routing.ParseWorkspaceSlug(input.Workspace)
			if err != nil {
				return nil, err
			}
			return service.Read(ctx, slug, a.check)
		}),
		typedTool("workspace_publication_set", "Publish or unpublish at the original reviewed publication revision. Publishing acquires the canonical relay readiness lease. Unpublishing the calling Workspace completes once and denies later public calls.", objectSchema(`"workspace":{"type":"string"},"published":{"type":"boolean"},"revision":{"type":"string"}`, "workspace", "published", "revision"), func(ctx context.Context, a authority, input struct {
			Workspace string `json:"workspace"`
			Published bool   `json:"published"`
			Revision  string `json:"revision"`
		}) (any, error) {
			slug, err := routing.ParseWorkspaceSlug(input.Workspace)
			if err != nil {
				return nil, err
			}
			return service.Set(ctx, slug, input.Published, input.Revision, a.check)
		}),
		typedTool("remote_addresses_list", "List the managed relay address. Does not provision a network path or imply publication.", objectSchema(``), func(context.Context, authority, struct{}) (any, error) {
			address := ""
			if service.RelayURL != nil {
				address = service.RelayURL()
			}
			return []struct {
				ID      string `json:"id"`
				URL     string `json:"url"`
				Managed bool   `json:"managed"`
			}{{"relay", address, true}}, nil
		}),
	}
}
