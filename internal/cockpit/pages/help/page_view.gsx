package help

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

const (
	communityURL = "https://discord.gg/UejYpMGmw"
	issueURL     = "https://github.com/swobuforge/swobu/issues/new"
)

// PageView renders the static help surface.
type PageView struct {
	Version           string
	DaemonVersion     string
	DiagnosticsStatus readmodel.DiagnosticsStatus
}

// View builds the help page from a loaded readmodel.
func View(model readmodel.HelpReadModel) *PageView {
	ds := model.DiagnosticsStatus
	if ds == 0 {
		ds = readmodel.DiagnosticsReady
	}
	return &PageView{
		Version:           model.Version,
		DaemonVersion:     model.DaemonVersion,
		DiagnosticsStatus: ds,
	}
}

func (v *PageView) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyDown, ui.SelectNext),
		tui.OnStop(tui.KeyUp, ui.SelectPrevious),
		tui.OnStop(tui.KeyEscape, func(event tui.KeyEvent) {
			if app := event.App(); app != nil {
				app.Stop()
			}
		}),
	}
}

func (v *PageView) versionValue() string {
	version := strings.TrimSpace(v.Version)
	if version == "" {
		version = "unknown"
	}
	daemon := strings.TrimSpace(v.DaemonVersion)
	if daemon == "" || daemon == version {
		return "swobu " + version
	}
	return "swobu " + daemon + " / " + version
}

func (v *PageView) diagnosticsValue() string {
	switch v.DiagnosticsStatus {
	case readmodel.DiagnosticsCopied:
		return "copied · paste into issue/Discord"
	case readmodel.DiagnosticsSaved:
		return "saved to /tmp/swobu-diagnostics.txt"
	case readmodel.DiagnosticsFailed:
		return "failed · run swobu doctor --copy"
	default:
		return "copy report context"
	}
}

func (v *PageView) diagnosticsAction() string {
	if v.DiagnosticsStatus == readmodel.DiagnosticsSaved {
		return "open \u21b5"
	}
	return "copy \u21b5"
}

func (v *PageView) diagnosticsPayloadText() string {
	payload := readmodel.DiagnosticsPayload{Version: v.Version}
	if v.DaemonVersion != "" {
		payload.DaemonVersion = v.DaemonVersion
	}
	return payload.Text()
}

func (v *PageView) onDiagnosticsCopied(result ui.CopyResult) {
	switch result.Status {
	case ui.CopyOK:
		v.DiagnosticsStatus = readmodel.DiagnosticsCopied
	case ui.CopySavedFile:
		v.DiagnosticsStatus = readmodel.DiagnosticsSaved
	default:
		v.DiagnosticsStatus = readmodel.DiagnosticsFailed
	}
}

// VersionRow displays the version line.
func VersionRowComponent(v *PageView) *ui.SelectableRow {
	return ui.NewSelectableRow("help:version", "version", v.versionValue(), "", nil)
}

// CommunityRow opens the community Discord.
func CommunityRowComponent(v *PageView) *ui.SelectableRow {
	return ui.LinkRowComponent("help:community", "community", "Discord", communityURL)
}

// IssueRow opens the GitHub issue tracker.
func IssueRowComponent(v *PageView) *ui.SelectableRow {
	return ui.LinkRowComponent("help:issue", "issue", "GitHub issue", issueURL)
}

// DiagnosticsRow copies or saves diagnostics.
func DiagnosticsRowComponent(v *PageView) *ui.SelectableRow {
	return ui.CopyPasteRowComponent(
		"help:diagnostics",
		"diagnostics",
		v.diagnosticsValue(),
		v.diagnosticsAction(),
		func() ui.CopyResult { return ui.CopyToClipboard(v.diagnosticsPayloadText()) },
		v.onDiagnosticsCopied,
	)
}

templ (v *PageView) Render() {
	<div class="flex-col w-full pl-2">
		<div class="flex-row w-full">
			<span>  help</span>
		</div>
		<br />
		<div class="flex-col w-full">
			@VersionRowComponent(v)
			@CommunityRowComponent(v)
			@IssueRowComponent(v)
			@DiagnosticsRowComponent(v)
		</div>
		<br />
	</div>
}
