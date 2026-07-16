package activity

import (
	"context"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// SectionView renders a single static activity row under an always-expanded
// header. V0: no click-through, no toggle, no focusable rows.
type SectionView struct {
	Workspace     readmodel.WorkspaceReadModel
	Ctx           context.Context
	ActivityQuery ports.ActivityQueries
}

func Section(workspace readmodel.WorkspaceReadModel, ctx context.Context, activityQuery ports.ActivityQueries) *SectionView {
	if ctx == nil {
		ctx = context.Background()
	}
	return &SectionView{
		Workspace:     workspace,
		Ctx:           ctx,
		ActivityQuery: activityQuery,
	}
}

func (s *SectionView) activityText() string {
	activity := s.Workspace.Activity
	if s.ActivityQuery != nil && !s.Workspace.IsDraft() {
		q, err := s.ActivityQuery.ListActivity(s.Ctx, ports.ListActivityRequest{WorkspaceID: s.Workspace.ID, Limit: 1})
		if err == nil {
			activity = q
		}
	}
	latest, ok := activity.LatestRow()
	if !ok {
		return ""
	}
	return latest.RowValue()
}

func (s *SectionView) Render(_ *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100.00),
	)
	root.AddChild(inertHeader())
	body := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100.00),
		tui.WithPaddingTRBL(0, 0, 0, ui.RowIndent),
	)
	if text := s.activityText(); text != "" {
		body.AddChild(inertRow("latest", text))
	} else if s.Workspace.IsDraft() {
		body.AddChild(inertRow("latest", "(no activity)"))
	} else {
		body.AddChild(inertRow("latest", "no requests yet"))
	}
	root.AddChild(body)
	return root
}

// inertHeader renders the section header with thesame indentation used by
// other section headers (2 spaces before label).
func inertHeader() *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100.00),
	)
	root.AddChild(tui.New(tui.WithWidth(2)))
	root.AddChild(tui.New(tui.WithText("activity ▾")))
	return root
}

// inertRow renders a label/value pair at the padded boundary set by the
// parent wrapper. Arrow spacer (RowIndent) aligns with selective row markers.
func inertRow(label string, value string) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100.00),
	)
	root.AddChild(tui.New(tui.WithWidth(2))) // arrow spacer
	root.AddChild(tui.New(tui.WithText(label), tui.WithWidth(18)))
	root.AddChild(tui.New(tui.WithText(value)))
	return root
}

func (s *SectionView) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*SectionView)
	if !ok {
		return
	}
	s.Workspace = f.Workspace
	s.Ctx = f.Ctx
	s.ActivityQuery = f.ActivityQuery
}

var _ tui.PropsUpdater = (*SectionView)(nil)
var _ tui.Component = (*SectionView)(nil)
