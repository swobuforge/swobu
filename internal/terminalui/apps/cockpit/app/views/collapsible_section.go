// Collapsible section views compose title rows and body content into sections
// that can be expanded or collapsed. The canonical form returns core.Node;
// retained wrappers are legacy bridges.
package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// CollapsibleSectionNode returns a pure core.Node collapsible section.
// open is read from model state; toggleAction is emitted when the user
// activates the title row.
func CollapsibleSectionNode(
	title string,
	open bool,
	toggleAction state.Action,
	body ...core.Node[state.Action],
) core.Node[state.Action] {
	cleanTitle := strings.TrimSpace(title) // swobu:io-string source=boundary
	var children []core.Node[state.Action]
	if len(body) == 0 {
		children = []core.Node[state.Action]{
			sectionStaticTitleNode(cleanTitle, open),
		}
	} else {
		indicator := "▸"
		verb := "open"
		if open {
			indicator = "▾"
			verb = "close"
		}
		titleNode := sectionToggleTitleNode(cleanTitle, indicator, verb, toggleAction)
		children = append([]core.Node[state.Action]{titleNode}, body...)
	}
	return core.Stack[state.Action](core.AxisVertical, children...).
		Key(core.K("section/" + cleanTitle)).
		Interaction(core.InteractionSpec[state.Action]{
			Focus: core.FocusSpec{Mode: core.FocusGroup},
		})
}

// NewCollapsibleSection is the retained bridge. Migrate callers to
// CollapsibleSectionNode.
func NewCollapsibleSection(
	title string,
	defaultOpen bool,
	_ string, // firstVerb — unused in bridge; only for signature compat
	summary retained.ViewSpec[state.Model],
	body ...retained.ViewSpec[state.Model],
) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		cleanTitle := strings.TrimSpace(title) // swobu:io-string source=boundary
		open := ctx.Model().SectionOpen[cleanTitle]
		var out retained.ViewSpec[state.Model]
		if len(body) == 0 {
			children := []retained.ViewSpec[state.Model]{
				retained.Named[state.Model]("title", sectionStaticTitleRow(cleanTitle, defaultOpen)),
			}
			if summary != nil {
				children = append(children, summary)
			}
			out = retained.VStack(ctx, children...)
		} else {
			titleRow := retained.Named[state.Model]("title", sectionToggleTitleRow(cleanTitle, open, func() []update.Action {
				return []update.Action{state.ToggleSectionOpen{Title: cleanTitle}, state.FocusNextAfterRebuildRequested{}}
			}))
			children := []retained.ViewSpec[state.Model]{titleRow}
			if open {
				children = append(children, body...)
			} else if summary != nil {
				children = append(children, summary)
			}
			out = EscClosableDisclosure(retained.VStack(ctx, children...), open, func() []update.Action {
				return []update.Action{
					state.ToggleSectionOpen{Title: cleanTitle},
					state.SetInteractionMode{Mode: state.InteractionModeNAV},
					state.SetFocusedRowAffordance{Verb: "open"},
				}
			})
		}
		return out
	})
}

func sectionToggleTitleNode(title, indicator, verb string, toggleAction state.Action) core.Node[state.Action] {
	line := strings.TrimSpace(title) + " " + indicator // swobu:io-string source=boundary
	return core.Action[state.Action](line, core.SignalEvent[state.Action]{
		Kind:  cockpitActionSignalKind,
		Event: toggleAction,
	}).Interaction(core.InteractionSpec[state.Action]{
		Focus:  core.FocusSpec{Mode: core.Focusable},
		Keymap: []core.KeyBindingSpec{{Pattern: core.KeyEnter(), Intent: core.IntentActivate}},
		Help:   []core.HelpBindingSpec{{Key: "↵", Label: verb}},
		Signals: []core.SignalEvent[state.Action]{
			{Kind: cockpitActionSignalKind, Event: toggleAction},
		},
		FocusSignals: []core.SignalEvent[state.Action]{
			{Kind: cockpitRowFocusSignalKind, Event: state.SetFocusedRowAffordance{Verb: verb}},
		},
	})
}

func sectionStaticTitleNode(title string, expanded bool) core.Node[state.Action] {
	indicator := "▸"
	if expanded {
		indicator = "▾"
	}
	return IndentLeftNode(InsetSection)(core.Text[state.Action](strings.TrimSpace(title) + " " + indicator)) // swobu:io-string source=boundary
}

func sectionToggleTitleRow(title string, expanded bool, onToggle func() []update.Action) retained.ViewSpec[state.Model] {
	title = strings.TrimSpace(title) // swobu:io-string source=boundary
	indicator := "▸"
	if expanded {
		indicator = "▾"
	}
	verb := "open"
	if expanded {
		verb = "close"
	}
	return toolkitviews.ListItemRowWithHooks[state.Model](
		title+" "+indicator,
		false,
		false,
		false,
		onToggle,
		nil,
		focusAffordance(verb, false),
	)
}

func sectionStaticTitleRow(title string, expanded bool) retained.ViewSpec[state.Model] {
	title = strings.TrimSpace(title) // swobu:io-string source=boundary
	indicator := "▸"
	if expanded {
		indicator = "▾"
	}
	return IndentLeft[state.Model](StaticTextLine(title+" "+indicator), InsetSection)
}

func staticSectionSummary(ctx *retained.Context[state.Model], title, summary string) retained.ViewSpec[state.Model] {
	return retained.VStack(ctx,
		sectionStaticTitleRow(title, false),
		SummaryRow(summary),
	)
}
