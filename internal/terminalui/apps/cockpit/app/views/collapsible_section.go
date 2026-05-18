package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func NewCollapsibleSection(
	title string,
	defaultOpen bool,
	firstVerb string,
	summary retained.ViewSpec[state.Model],
	body ...retained.ViewSpec[state.Model],
) retained.ViewSpec[state.Model] {
	cleanTitle := strings.TrimSpace(title) // swobu:io-string source=boundary
	return retained.Named[state.Model](cleanTitle, retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		open, setOpen := retained.UseState(ctx, func() bool { return defaultOpen })
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
			titleRow := retained.Named[state.Model]("title", sectionToggleTitleRow(cleanTitle, open, func() []update.Action {
				if open {
					return closeSection()
				}
				setOpen(true)
				verb := strings.TrimSpace(firstVerb) // swobu:io-string source=boundary
				if verb == "" {
					verb = "act"
				}
				return []update.Action{
					state.FocusNextAfterRebuildRequested{},
					state.SetFocusedRowAffordance{Verb: verb},
				}
			}))

			children := []retained.ViewSpec[state.Model]{titleRow}
			if open {
				children = append(children, body...)
			} else if summary != nil {
				children = append(children, summary)
			}
			out = EscClosableDisclosure(retained.VStack(ctx, children...), open, closeSection)
		}
		return out
	}))
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
	return IndentLeft[state.Model](StaticTextLine[state.Model](title+" "+indicator), InsetSection)
}

func staticSectionSummary(ctx *retained.Context[state.Model], title, summary string) retained.ViewSpec[state.Model] {
	return retained.VStack(ctx,
		sectionStaticTitleRow(title, false),
		SummaryRow(summary),
	)
}
