package adapters

import (
	"sort"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func (a *LiveOperatorAdapter) baseDiagnosticsPayload() readmodel.DiagnosticsPayload {
	return readmodel.DiagnosticsPayload{
		Version:       controlplane.SwobuVersion(),
		DaemonVersion: "",
	}
}

func diagnosticsPayloadFromEndpoints(base readmodel.DiagnosticsPayload, endpoints []operatorclient.EndpointData, activityByWorkspace map[string]int) readmodel.DiagnosticsPayload {
	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].Name < endpoints[j].Name
	})
	payload := base
	payload.Workspaces = make([]readmodel.DiagnosticsWorkspacePayload, 0, len(endpoints))
	totalActivity := 0
	for _, endpoint := range endpoints {
		workspace := readmodel.DiagnosticsWorkspacePayload{
			Name:          strings.TrimSpace(endpoint.Name),
			Routes:        diagnosticsRoutesFromEndpoint(endpoint),
			ActivityCount: activityByWorkspace[endpoint.Name],
		}
		totalActivity += workspace.ActivityCount
		payload.Workspaces = append(payload.Workspaces, workspace)
	}
	payload.ActivityCount = totalActivity
	return payload
}

func diagnosticsRoutesFromEndpoint(endpoint operatorclient.EndpointData) []readmodel.DiagnosticsRoutePayload {
	routes := routesFromEndpoint(endpoint)
	out := make([]readmodel.DiagnosticsRoutePayload, 0, len(routes))
	for _, route := range routes {
		targets := make([]readmodel.DiagnosticsTargetPayload, 0, len(route.Targets))
		for _, target := range route.Targets {
			targets = append(targets, readmodel.DiagnosticsTargetPayload{Name: firstNonEmpty(target.Name, string(target.ID))})
		}
		out = append(out, readmodel.DiagnosticsRoutePayload{
			Name:    route.ModelName,
			Default: route.Default,
			Targets: targets,
		})
	}
	return out
}
