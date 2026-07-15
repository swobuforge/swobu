package readmodel

import (
	"encoding/json"
	"fmt"
)

// HelpReadModel is the support and orientation snapshot for the help tab.
//
// Help is not a keyboard manual. It exposes version context, human exits, and a
// diagnostics-copy affordance while keeping detailed diagnostics off screen.
type HelpReadModel struct {
	Version           string
	CockpitVersion    string
	Commit            string
	DocsURL           string
	CommunityURL      string
	IssueURL          string
	DiagnosticsStatus DiagnosticsStatus
	Diagnostics       DiagnosticsPayload
}

// DiagnosticsPayload is the explicit safe-list support context copied from the
// help tab. It intentionally carries names and counts only: no headers, bodies,
// credential references, env vars, auth headers, base URLs, or provider config
// documents.
type DiagnosticsPayload struct {
	Version       string                        `json:"version,omitempty"`
	Cockpit       string                        `json:"cockpit,omitempty"`
	Commit        string                        `json:"commit,omitempty"`
	Daemon        string                        `json:"daemon,omitempty"`
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

// VersionValue returns the compact support version row value.
func (h HelpReadModel) VersionValue() string {
	version := h.Version
	if version == "" {
		version = "unknown"
	}
	cockpit := h.CockpitVersion
	if cockpit == "" {
		cockpit = "cockpit v2"
	}
	commit := h.Commit
	if commit == "" {
		commit = "commit unavailable"
	} else {
		commit = "commit " + commit
	}
	return version + " · " + cockpit + " · " + commit
}

// DocsValue returns the human-readable docs destination.
func (h HelpReadModel) DocsValue() string {
	if h.DocsURL == "" {
		return "unavailable · link missing"
	}
	return h.DocsURL
}

// CommunityValue returns clean community copy without exposing long invite URLs.
func (h HelpReadModel) CommunityValue() string {
	if h.CommunityURL == "" {
		return "unavailable · link missing"
	}
	return "Discord"
}

// IssueValue returns clean issue-copy without exposing the full issue URL.
func (h HelpReadModel) IssueValue() string {
	if h.IssueURL == "" {
		return "unavailable · link missing"
	}
	return "GitHub issue"
}

// DiagnosticsValue returns the current diagnostics support action state.
func (h HelpReadModel) DiagnosticsValue() string {
	switch h.DiagnosticsStatus {
	case DiagnosticsCopied:
		return "copied · paste into issue/Discord"
	case DiagnosticsSaved:
		return "saved to /tmp/swobu-diagnostics.txt"
	case DiagnosticsFailed:
		return "failed · run swobu doctor --copy"
	default:
		return "copy report context"
	}
}

// DiagnosticsAction returns the action label for the diagnostics row.
func (h HelpReadModel) DiagnosticsAction() string {
	if h.DiagnosticsStatus == DiagnosticsSaved {
		return "open ↵"
	}
	return "copy ↵"
}

func (h HelpReadModel) DiagnosticsCopyText() string {
	payload := h.Diagnostics
	if payload.Version == "" {
		payload.Version = h.Version
	}
	if payload.Cockpit == "" {
		payload.Cockpit = h.CockpitVersion
	}
	if payload.Commit == "" {
		payload.Commit = h.Commit
	}
	return payload.Text()
}

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
