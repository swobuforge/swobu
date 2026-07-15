package help

import (
	"context"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

type PageView struct {
	Model             readmodel.HelpReadModel
	DiagnosticsStatus *tui.State[readmodel.DiagnosticsStatus]
	Ctx               context.Context
	Actions           ports.HelpActions
}

func ViewWithContext(model readmodel.HelpReadModel, ctx context.Context, actions ports.HelpActions) *PageView {
	if model.DiagnosticsStatus == 0 {
		model.DiagnosticsStatus = readmodel.DiagnosticsReady
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &PageView{
		Model:             model,
		DiagnosticsStatus: tui.NewState(model.DiagnosticsStatus),
		Ctx:               ctx,
		Actions:           actions,
	}
}

func (v *PageView) OpenDocs() {
	if v.Actions != nil {
		_ = v.Actions.OpenDocs(v.Ctx)
	}
}

func (v *PageView) OpenCommunity() {
	if v.Actions != nil {
		_ = v.Actions.OpenCommunity(v.Ctx)
	}
}

func (v *PageView) OpenIssue() {
	if v.Actions != nil {
		_ = v.Actions.OpenIssue(v.Ctx)
	}
}

func (v *PageView) CopyDiagnostics() {
	if v.Actions == nil {
		return
	}
	result, err := v.Actions.CopyDiagnostics(v.Ctx)
	if err != nil {
		v.DiagnosticsStatus.Set(readmodel.DiagnosticsFailed)
		return
	}
	switch result.Status {
	case ports.DiagnosticsCopyCopied:
		v.DiagnosticsStatus.Set(readmodel.DiagnosticsCopied)
	case ports.DiagnosticsCopySaved:
		v.DiagnosticsStatus.Set(readmodel.DiagnosticsSaved)
	default:
		v.DiagnosticsStatus.Set(readmodel.DiagnosticsFailed)
	}
}

func (v *PageView) currentModel() readmodel.HelpReadModel {
	model := v.Model
	model.DiagnosticsStatus = v.DiagnosticsStatus.Get()
	return model
}

func (v *PageView) VersionValue() string      { return v.currentModel().VersionValue() }
func (v *PageView) DocsValue() string         { return v.currentModel().DocsValue() }
func (v *PageView) DocsURL() string           { return v.currentModel().DocsURL }
func (v *PageView) CommunityValue() string    { return v.currentModel().CommunityValue() }
func (v *PageView) CommunityURL() string      { return v.currentModel().CommunityURL }
func (v *PageView) IssueValue() string        { return v.currentModel().IssueValue() }
func (v *PageView) IssueURL() string          { return v.currentModel().IssueURL }
func (v *PageView) DiagnosticsValue() string  { return v.currentModel().DiagnosticsValue() }
func (v *PageView) DiagnosticsAction() string { return v.currentModel().DiagnosticsAction() }

templ (v *PageView) Render() {
	<div class="flex-col w-full" deps={v.DiagnosticsStatus}>
		<div class="flex-row">
			<span class="w-2"></span>
			<span>help</span>
		</div>
		<br />
		@HelpRow("version", v.VersionValue(), "")
		<br />
		@HelpActionRow("docs", v.DocsValue(), linkAction(v.DocsURL()), func() { v.OpenDocs() })
		@HelpActionRow("community", v.CommunityValue(), linkAction(v.CommunityURL()), func() { v.OpenCommunity() })
		@HelpActionRow("issue", v.IssueValue(), linkAction(v.IssueURL()), func() { v.OpenIssue() })
		@HelpActionRow("diagnostics", v.DiagnosticsValue(), v.DiagnosticsAction(), func() { v.CopyDiagnostics() })
		<br />
		<br />
	</div>
}

templ HelpRow(label string, value string, action string) {
	<div class="flex-row w-full">
		<span class="w-5"></span>
		<span class="w-18">{label}</span>
		<span class="w-36">{value}</span>
		<span>{action}</span>
	</div>
}

templ HelpActionRow(label string, value string, action string, activate func()) {
	<div class="flex-row w-full focusable" onActivate={activate}>
		<span class="w-5"></span>
		<span class="w-18">{label}</span>
		<span class="w-36">{value}</span>
		<span>{action}</span>
	</div>
}

func linkAction(url string) string {
	if url == "" {
		return ""
	}
	return "open ↵"
}
