package readmodel

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
