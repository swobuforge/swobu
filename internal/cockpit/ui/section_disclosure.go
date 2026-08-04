package ui

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui/interaction"
)

// SectionDisclosure is a focusable, collapsible section header.
// Sections mount it and read Expanded state to conditionally render children.
//
// Enter toggles expand/collapse. Escape collapses when the header is focused.
// Escape from section children is handled by the containing section scope.
type SectionDisclosure struct {
	disclosure *interaction.Disclosure
	Label      string
	Expanded   *tui.State[bool]
	// AutoFocus seeds this disclosure as the selected control on first mount.
	AutoFocus bool
	// OnToggle lets a section react to disclosure state changes without adding
	// render-side effects.
	OnToggle func(expanded bool)
}

// NewSectionDisclosure builds a mountable disclosure header with focus marker
// and self-toggle activation. The caller owns Expanded state and child visibility.
func NewSectionDisclosure(id, label string, expanded *tui.State[bool]) *SectionDisclosure {
	d := &SectionDisclosure{
		Label:    label,
		Expanded: expanded,
	}
	d.disclosure = interaction.NewDisclosure(d.propsWithID(id))
	return d
}

func (d *SectionDisclosure) toggle() {
	d.Expanded.Set(!d.Expanded.Get())
	if d.OnToggle != nil {
		d.OnToggle(d.Expanded.Get())
	}
}

func (d *SectionDisclosure) Render(app *tui.App) *tui.Element {
	d.disclosure.SetRenderProps(d.props())
	opts := append(d.disclosure.ShellOptions(), tui.WithOnActivate(d.toggle))
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100.00),
	)
	for _, opt := range opts {
		opt(root)
	}
	root.AddChild(tui.New(tui.WithText(d.disclosure.Marker()), tui.WithWidth(2)))
	indicator := " ▾"
	if !d.Expanded.Get() {
		indicator = " ▸"
	}
	root.AddChild(tui.New(tui.WithText(d.Label + indicator)))
	d.disclosure.BindElement(root)
	return root
}

func (d *SectionDisclosure) Init() func() { return d.disclosure.Init() }

func (d *SectionDisclosure) KeyMap() tui.KeyMap {
	return d.disclosure.KeyMap()
}

func (d *SectionDisclosure) BindApp(app *tui.App) { d.disclosure.BindApp(app) }

func (d *SectionDisclosure) UnbindApp() { d.disclosure.UnbindApp() }

func (d *SectionDisclosure) IsFocused() bool { return d.disclosure.IsFocused() }

func (d *SectionDisclosure) props() interaction.DisclosureProps {
	return d.propsWithID(d.disclosure.Props().ID)
}

func (d *SectionDisclosure) propsWithID(id string) interaction.DisclosureProps {
	return interaction.DisclosureProps{
		ID:        id,
		Label:     d.Label,
		Expanded:  d.Expanded,
		AutoFocus: d.AutoFocus,
		OnExpand: func(interaction.Context) {
			if d.OnToggle != nil {
				d.OnToggle(true)
			}
		},
		OnCollapse: func(interaction.Context) {
			if d.OnToggle != nil {
				d.OnToggle(false)
			}
		},
	}
}

var (
	_ tui.Component   = (*SectionDisclosure)(nil)
	_ tui.KeyListener = (*SectionDisclosure)(nil)
	_ tui.AppBinder   = (*SectionDisclosure)(nil)
	_ tui.Initializer = (*SectionDisclosure)(nil)
)
