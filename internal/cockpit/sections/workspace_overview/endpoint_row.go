package workspace_overview

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

func endpointRowKey(s *SectionView) string { return "endpoint:" + workspaceIdentity(s) }

func endpointAction(s *SectionView) string {
	if s.CopiedEndpoint.Get() {
		return "copied"
	}
	return "copy ↵"
}

// EndpointRowComponent mounts the hero endpoint row: two visual lines
// (compatibility badges + URL) as a single selectable action target.
func EndpointRowComponent(s *SectionView) tui.Component {
	return &endpointRowView{s: s, target: ui.NewActionTarget(endpointRowKey(s), s.copyEndpoint)}
}

type endpointRowView struct {
	target *ui.ActionTarget
	s      *SectionView
}

func (r *endpointRowView) BindApp(app *tui.App) {
	r.target.BindApp(app)
}

func (r *endpointRowView) UnbindApp() {
	r.target.UnbindApp()
}

func (r *endpointRowView) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*endpointRowView)
	if !ok {
		return
	}
	r.s = f.s
}

func (r *endpointRowView) IsFocused() bool {
	return r.target.IsFocused()
}

func (r *endpointRowView) KeyMap() tui.KeyMap {
	return r.target.KeyMap(r.s.copyEndpoint, nil)
}

func (r *endpointRowView) Render(*tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
	)
	opts := append(r.target.ShellOptions(), tui.WithOnActivate(r.s.copyEndpoint))
	row := ui.ActionRow(r.target.Marker(), "endpoint", r.s.Model.ClientBaseURL, endpointAction(r.s), opts...)
	r.target.BindElement(row)
	root.AddChild(row)

	detail := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100),
	)
	detail.AddChild(tui.New(tui.WithWidth(20)))
	detail.AddChild(tui.New(tui.WithText("OpenAI · Anthropic"), tui.WithFlexGrow(1)))
	root.AddChild(detail)
	return root
}

var (
	_ tui.Component    = (*endpointRowView)(nil)
	_ tui.KeyListener  = (*endpointRowView)(nil)
	_ tui.AppBinder    = (*endpointRowView)(nil)
	_ tui.PropsUpdater = (*endpointRowView)(nil)
)
