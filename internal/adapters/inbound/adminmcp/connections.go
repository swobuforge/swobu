package adminmcp

import (
	"context"

	connectionapp "github.com/swobuforge/swobu/internal/app/operator/connections"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/mcp"
)

// ConnectionTools uses the same redacted queries and revision-bound commands
// as WebUI. Probes are bootstrap-owned adapters with the existing network policy.
func ConnectionTools(queries *connectionapp.Queries, commands *connectionapp.Commands, probe connectionapp.Probe) []mcp.AdminTool {
	type idInput struct {
		ID string `json:"id"`
	}
	type checked struct {
		ID       string `json:"id"`
		Revision string `json:"revision"`
	}
	const id = `"id":{"type":"string"}`
	const revision = `"revision":{"type":"string","pattern":"^[1-9][0-9]*$"}`
	return []mcp.AdminTool{
		typedTool("connections_list", "List reusable Connections using secret-free read projections. Does not execute or refresh a source.", objectSchema(`"kind":{"type":"string","enum":["","model","mcp","search"]},"query":{"type":"string"}`, "kind", "query"), func(ctx context.Context, _ authority, input struct {
			Kind  connections.Kind `json:"kind"`
			Query string           `json:"query"`
		}) (any, error) {
			return queries.List(ctx, connectionapp.Filter{Kind: input.Kind, Query: input.Query})
		}),
		typedTool("connection_get", "Read public Connection metadata and its original revision, without credential references, stdio arguments or environment values.", objectSchema(id, "id"), func(ctx context.Context, _ authority, input idInput) (any, error) {
			identity, err := connectionidentity.Parse(input.ID)
			if err != nil {
				return nil, err
			}
			return queries.Get(ctx, identity)
		}),
		typedTool("connection_references", "Inspect current Workspace consumers before reviewing deletion.", objectSchema(id, "id"), func(ctx context.Context, _ authority, input idInput) (any, error) {
			identity, err := connectionidentity.Parse(input.ID)
			if err != nil {
				return nil, err
			}
			return queries.References(ctx, identity)
		}),
		typedTool("connection_check", "Run an explicitly requested upstream probe at the reviewed Connection revision. Network policy and credential custody remain with the canonical probe adapter.", objectSchema(id+`,`+revision, "id", "revision"), func(ctx context.Context, a authority, input checked) (any, error) {
			identity, err := connectionidentity.Parse(input.ID)
			if err != nil {
				return nil, err
			}
			reviewed, err := connections.ParseRevision(input.Revision)
			if err != nil {
				return nil, err
			}
			return commands.Check(ctx, identity, reviewed, a.check, probe)
		}),
		typedTool("connection_delete", "Delete an unreferenced Connection at the original reviewed revision. Refuses deletion while any Workspace references it.", objectSchema(id+`,`+revision, "id", "revision"), func(ctx context.Context, a authority, input checked) (any, error) {
			identity, err := connectionidentity.Parse(input.ID)
			if err != nil {
				return nil, err
			}
			reviewed, err := connections.ParseRevision(input.Revision)
			if err != nil {
				return nil, err
			}
			err = commands.Delete(ctx, identity, reviewed, "mcp:"+a.principal.String()+":connection.delete", a.check)
			return struct {
				Deleted bool `json:"deleted"`
			}{err == nil}, err
		}),
	}
}
