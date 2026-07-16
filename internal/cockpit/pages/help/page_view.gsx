package help

import (
	"context"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

type PageView struct {
	Model             *tui.State[readmodel.HelpReadModel]
	DiagnosticsStatus *tui.State[readmodel.DiagnosticsStatus]
	Ctx               *tui.State[context.Context]
	Actions           *tui.State[ports.HelpActions]
}

func helpModelKey(model readmodel.HelpReadModel) string {
	return model.Version + "\x1f" +
		model.CockpitVersion + "\x1f" +
		model.Commit + "\x1f" +
		model.DocsURL + "\x1f" +
		model.CommunityURL + "\x1f" +
		model.IssueURL + "\x1f" +
		model.Diagnostics.Text()
}

func normalizeDiagnosticsStatus(status readmodel.DiagnosticsStatus) readmodel.DiagnosticsStatus {
	if status == 0 {
		return readmodel.DiagnosticsReady
	}
	return status
}

func ViewWithContext(model readmodel.HelpReadModel, ctx context.Context, actions ports.HelpActions) *PageView {
	model.DiagnosticsStatus = normalizeDiagnosticsStatus(model.DiagnosticsStatus)
	if ctx == nil {
		ctx = context.Background()
	}
	return &PageView{
		Model:             tui.NewState(model),
		DiagnosticsStatus: tui.NewState(model.DiagnosticsStatus),
		Ctx:               tui.NewState(ctx),
		Actions:           tui.NewState(actions),
	}
}

func (v *PageView) OpenDocs() {
	if actions := v.Actions.Get(); actions != nil {
		_ = actions.OpenDocs(v.Ctx.Get())
	}
}

func (v *PageView) OpenCommunity() {
	if actions := v.Actions.Get(); actions != nil {
		_ = actions.OpenCommunity(v.Ctx.Get())
	}
}

func (v *PageView) OpenIssue() {
	if actions := v.Actions.Get(); actions != nil {
		_ = actions.OpenIssue(v.Ctx.Get())
	}
}

func (v *PageView) CopyDiagnostics() {
	actions := v.Actions.Get()
	if actions == nil {
		return
	}
	result, err := actions.CopyDiagnostics(v.Ctx.Get())
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
	model := v.Model.Get()
	model.DiagnosticsStatus = v.DiagnosticsStatus.Get()
	return model
}

func (v *PageView) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*PageView)
	if !ok {
		return
	}
	identityChanged := helpModelKey(v.Model.Get()) != helpModelKey(f.Model.Get())
	v.Model.Set(f.Model.Get())
	v.Ctx.Set(f.Ctx.Get())
	v.Actions.Set(f.Actions.Get())
	if identityChanged {
		v.DiagnosticsStatus.Set(readmodel.DiagnosticsReady)
	}
}

func (v *PageView) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyUp, ui.MovePrev),
		tui.OnStop(tui.KeyDown, ui.MoveNext),
	}
}

func helpActionRowKey(label string) string {
	return "help-action:" + label
}

func HelpActionRow(label string, value string, action string, activate func()) *ui.SelectableRow {
	return ui.NewSelectableRow(
		helpActionRowKey(label),
		label,
		value,
		action,
		activate,
	)
}

templ (v *PageView) Render() {
	<div class="flex-col w-full" deps={v.DiagnosticsStatus}>
		<div class="flex-row">
			<span class="w-2"></span>
			<span>help</span>
		</div>
		<br />
		@HelpRow("version", v.currentModel().VersionValue(), "")
		<br />
		<div key={helpActionRowKey("docs")} class="w-full">
			@HelpActionRow("docs", v.currentModel().DocsValue(), linkAction(v.currentModel().DocsURL), func() { v.OpenDocs() })
		</div>
		<div key={helpActionRowKey("community")} class="w-full">
			@HelpActionRow("community", v.currentModel().CommunityValue(), linkAction(v.currentModel().CommunityURL), func() { v.OpenCommunity() })
		</div>
		<div key={helpActionRowKey("issue")} class="w-full">
			@HelpActionRow("issue", v.currentModel().IssueValue(), linkAction(v.currentModel().IssueURL), func() { v.OpenIssue() })
		</div>
		<div key={helpActionRowKey("diagnostics")} class="w-full">
			@HelpActionRow("diagnostics", v.currentModel().DiagnosticsValue(), v.currentModel().DiagnosticsAction(), func() { v.CopyDiagnostics() })
		</div>
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

func linkAction(url string) string {
	if url == "" {
		return ""
	}
	return "open ↵"
}
