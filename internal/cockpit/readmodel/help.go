package readmodel

import (
	"encoding/json"
	"fmt"
)

// HelpReadModel is the support and orientation snapshot for the help tab.
//
// Version is the CLI build version; DaemonVersion is the daemon's version.
// Diagnostics holds runtime workspace/route/activity context copied to the
// operator's clipboard.
type HelpReadModel struct {
	Version           string
	DaemonVersion     string
	DiagnosticsStatus DiagnosticsStatus
	Diagnostics       DiagnosticsPayload
}

// DiagnosticsPayload is the explicit safe-list support context copied from the
// help tab. It intentionally carries names and counts only: no headers, bodies,
// credential references, env vars, auth headers, base URLs, or provider config
// documents.
type DiagnosticsPayload struct {
	Version       string                        `json:"version,omitempty"`
	DaemonVersion string                        `json:"daemon_version,omitempty"`
	ConfigPath    string                        `json:"config_path,omitempty"`
	Workspaces    []DiagnosticsWorkspacePayload `json:"workspaces,omitempty"`
	ActivityCount int                           `json:"activity_count,omitempty"`
}

type DiagnosticsWorkspacePayload struct {
	Name          string                    `json:"name"`
	Routes        []DiagnosticsRoutePayload `json:"routes,omitempty"`
	ActivityCount int                       `json:"activity_count,omitempty"`
}

type DiagnosticsRoutePayload struct {
	Name    string                     `json:"name"`
	Default bool                       `json:"default,omitempty"`
	Targets []DiagnosticsTargetPayload `json:"targets,omitempty"`
}

type DiagnosticsTargetPayload struct {
	Name string `json:"name"`
}

// DiagnosticsStatus describes the visible result of the diagnostics copy action.
type DiagnosticsStatus int

const (
	DiagnosticsReady DiagnosticsStatus = iota
	DiagnosticsCopied
	DiagnosticsSaved
	DiagnosticsFailed
)

func (p DiagnosticsPayload) Text() string {
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func (p DiagnosticsPayload) Summary() string {
	workspaces := len(p.Workspaces)
	routes := 0
	targets := 0
	for _, workspace := range p.Workspaces {
		routes += len(workspace.Routes)
		for _, route := range workspace.Routes {
			targets += len(route.Targets)
		}
	}
	return fmt.Sprintf("%d workspaces · %d routes · %d targets · %d activity", workspaces, routes, targets, p.ActivityCount)
}
