package ui

import tui "github.com/grindlemire/go-tui"

// SectionDisclosure is a focusable, collapsible section header.
// Sections mount it and read Expanded state to conditionally render children.
//
// Enter toggles expand/collapse. Escape collapses when the header is focused.
// Escape from section children is handled by the containing section scope.
type SectionDisclosure struct {
	SelectBase
	Label    string
	Expanded *tui.State[bool]
	// OnToggle lets a section react to disclosure state changes without adding
	// render-side effects.
	OnToggle func(expanded bool)
}

// NewSectionDisclosure builds a mountable disclosure header with focus marker
// and self-toggle activation. The caller owns Expanded state and child visibility.
func NewSectionDisclosure(id, label string, expanded *tui.State[bool]) *SectionDisclosure {
	return &SectionDisclosure{
		SelectBase: NewSelectBase(id),
		Label:      label,
		Expanded:   expanded,
	}
}

func (d *SectionDisclosure) toggle() {
	d.Expanded.Set(!d.Expanded.Get())
	if d.OnToggle != nil {
		d.OnToggle(d.Expanded.Get())
	}
}

func (d *SectionDisclosure) Render(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100.00),
		tui.WithFocusable(true),
		tui.WithOnFocus(d.OnFocus),
		tui.WithOnBlur(d.OnBlur),
		tui.WithOnActivate(d.toggle),
	)
	root.AddChild(tui.New(tui.WithText(d.Arrow()), tui.WithWidth(2)))
	indicator := " ▾"
	if !d.Expanded.Get() {
		indicator = " ▸"
	}
	root.AddChild(tui.New(tui.WithText(d.Label + indicator)))
	if d.Ref != nil {
		d.Ref.Set(root)
	}
	return root
}

func (d *SectionDisclosure) KeyMap() tui.KeyMap {
	return d.WithTraversal(ActivateFocused(func(tui.KeyEvent) {
		d.toggle()
	}))
}

var (
	_ tui.Component   = (*SectionDisclosure)(nil)
	_ tui.KeyListener = (*SectionDisclosure)(nil)
	_ tui.AppBinder   = (*SectionDisclosure)(nil)
)
