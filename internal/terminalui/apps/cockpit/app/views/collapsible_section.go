package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
)

func NewCollapsibleSection(
	title string,
	defaultOpen bool,
	firstVerb string,
	summary view.ViewSpec[state.Model],
	body ...view.ViewSpec[state.Model],
) view.ViewSpec[state.Model] {
	cleanTitle := strings.TrimSpace(title)
	return view.Named[state.Model](cleanTitle, view.Build[state.Model](func(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
		open, setOpen := view.UseState(ctx, func() bool { return defaultOpen })
		var out view.ViewSpec[state.Model]
		if len(body) == 0 {
			children := []view.ViewSpec[state.Model]{
				view.Named[state.Model]("title", sectionStaticTitleRow(cleanTitle, defaultOpen)),
			}
			if summary != nil {
				children = append(children, summary)
			}
			out = view.VStack(ctx, children...)
		} else {
			closeSection := func() []update.Action {
				if !open {
					return nil
				}
				setOpen(false)
				return []update.Action{
					state.SetInteractionMode{Mode: state.InteractionModeNAV},
					state.SetFocusedRowAffordance{Verb: "open"},
				}
			}
			titleRow := view.Named[state.Model]("title", sectionToggleTitleRow(cleanTitle, open, func() []update.Action {
				if open {
					return closeSection()
				}
				setOpen(true)
				verb := strings.TrimSpace(firstVerb)
				if verb == "" {
					verb = "act"
				}
				return []update.Action{
					state.FocusNextAfterRebuildRequested{},
					state.SetFocusedRowAffordance{Verb: verb},
				}
			}))

			children := []view.ViewSpec[state.Model]{titleRow}
			if open {
				children = append(children, body...)
			} else if summary != nil {
				children = append(children, summary)
			}
			out = EscClosableDisclosure(view.VStack(ctx, children...), open, closeSection)
		}
		return out
	}))
}

func sectionToggleTitleRow(title string, expanded bool, onToggle func() []update.Action) view.ViewSpec[state.Model] {
	title = strings.TrimSpace(title)
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

func sectionStaticTitleRow(title string, expanded bool) view.ViewSpec[state.Model] {
	title = strings.TrimSpace(title)
	indicator := "▸"
	if expanded {
		indicator = "▾"
	}
	return IndentLeft[state.Model](StaticTextLine[state.Model](title+" "+indicator), InsetSection)
}

func staticSectionSummary(ctx *view.Context[state.Model], title, summary string) view.ViewSpec[state.Model] {
	return view.VStack(ctx,
		sectionStaticTitleRow(title, false),
		SummaryRow(summary),
	)
}
