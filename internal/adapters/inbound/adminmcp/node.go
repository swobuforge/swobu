package adminmcp

import (
	"context"

	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/nodeauthority"
)

func NodeTools(read func(context.Context) (nodeauthority.Overview, error), state func() (string, error)) []mcp.AdminTool {
	return []mcp.AdminTool{typedTool("node_status", "Read the committed Node revision, management mode and resource counts. Never returns configuration bytes or credential references.", objectSchema(""), func(ctx context.Context, _ authority, _ struct{}) (any, error) {
		overview, err := read(ctx)
		if err != nil {
			return nil, err
		}
		health, err := state()
		return struct {
			nodeauthority.Overview
			State                string `json:"state"`
			ControlPlaneProtocol int    `json:"control_plane_protocol"`
			SwobuVersion         string `json:"swobu_version"`
		}{overview, health, controlplane.Protocol, controlplane.SwobuVersion()}, err
	})}
}
