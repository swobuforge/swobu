package cockpit

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

type cockpitBodyViewport struct {
	cockpit *Cockpit
}

func CockpitBodyViewport(c *Cockpit) *cockpitBodyViewport {
	return &cockpitBodyViewport{cockpit: c}
}

func (v *cockpitBodyViewport) Render(app *tui.App) *tui.Element {
	c := v.cockpit
	root := tui.New(
		tui.WithFlexGrow(1),
		tui.WithWidthPercent(100),
		tui.WithScrollable(tui.ScrollVertical),
		tui.WithScrollOffset(0, c.BodyViewport.ScrollY.Get()),
		tui.WithScrollbarStyle(tui.NewStyle().Dim()),
		tui.WithScrollbarThumbStyle(tui.NewStyle()),
	)
	c.BodyViewport.Ref.Set(root)

	switch c.activeModel().ActivePage {
	case readmodel.CockpitHelpPage:
		addMountedOrRendered(root, app, v, 0, "", func() tui.Component {
			return ActiveHelpPage(c)
		})
	case readmodel.CockpitWorkspacePage:
		container := tui.New(tui.WithWidthPercent(100))
		addMountedOrRendered(container, app, v, 1, activeWorkspaceMountKey(c.activeModel()), func() tui.Component {
			return ActiveWorkspacePage(c, c.activeModel())
		})
		root.AddChild(container)
	}

	return root
}

func (v *cockpitBodyViewport) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*cockpitBodyViewport)
	if !ok {
		return
	}
	v.cockpit = f.cockpit
}

func addMountedOrRendered(parent *tui.Element, app *tui.App, owner tui.Component, index int, key string, child func() tui.Component) {
	if app != nil {
		mountKey := any(index)
		if key != "" {
			mountKey = tui.MountKey(index, key)
		}
		parent.AddChild(app.Mount(owner, mountKey, child))
		return
	}
	component := child()
	if component == nil {
		return
	}
	parent.AddChild(component.Render(nil))
}

var _ tui.Component = (*cockpitBodyViewport)(nil)
var _ tui.PropsUpdater = (*cockpitBodyViewport)(nil)
