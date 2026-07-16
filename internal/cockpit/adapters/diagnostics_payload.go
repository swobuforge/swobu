package adapters

import (
	"context"
	"fmt"
	"sort"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/platform/browser"
	"github.com/swobuforge/swobu/internal/platform/clipboard"
)

func (a *LiveOperatorAdapter) CopyDiagnostics(ctx context.Context) (ports.DiagnosticsCopyResult, error) {
	endpoints, err := a.client.ListEndpoints(ctx)
	if err != nil {
		return ports.DiagnosticsCopyResult{Status: ports.DiagnosticsCopyFailed}, adapterFailure("copy diagnostics", err)
	}
	payload := diagnosticsPayloadFromEndpoints(a.baseDiagnosticsPayload(), endpoints, a.diagnosticsActivityCounts(ctx, endpoints))
	text := payload.Text()

	copy := a.copyText
	if copy == nil {
		copy = clipboard.TryWriteText
	}
	cpOk, cpErr := copy(text)
	if cpOk {
		return ports.DiagnosticsCopyResult{
			Status: ports.DiagnosticsCopyCopied,
			Text:   text,
		}, nil
	}

	path, ferr := clipboard.WriteTempFileFallback("", "swobu-diagnostics-", text)
	if ferr != nil {
		return ports.DiagnosticsCopyResult{
			Status: ports.DiagnosticsCopyFailed,
			Text:   text,
		}, fmt.Errorf("clipboard copy failed: %w; fallback file: %v", cpErr, ferr)
	}

	return ports.DiagnosticsCopyResult{
		Status: ports.DiagnosticsCopySaved,
		Text:   text,
		Path:   path,
	}, nil
}

func (a *LiveOperatorAdapter) OpenDocs(context.Context) error {
	open := a.openURL
	if open == nil {
		open = browser.Open
	}
	return open(a.helpDocsURL)
}

func (a *LiveOperatorAdapter) OpenCommunity(context.Context) error {
	open := a.openURL
	if open == nil {
		open = browser.Open
	}
	return open(a.helpCommunityURL)
}

func (a *LiveOperatorAdapter) OpenIssue(context.Context) error {
	open := a.openURL
	if open == nil {
		open = browser.Open
	}
	return open(a.helpIssueURL)
}

func (a *LiveOperatorAdapter) baseDiagnosticsPayload() readmodel.DiagnosticsPayload {
	return readmodel.DiagnosticsPayload{
		Version: "swobu",
		Cockpit: "cockpit v2",
		Daemon:  a.daemonURL,
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
			Name:          strings.TrimSpace(endpoint.Name), // swobu:io-string source=boundary
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

var _ ports.HelpActions = (*LiveOperatorAdapter)(nil)
