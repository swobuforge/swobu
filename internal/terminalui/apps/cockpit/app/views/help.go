package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
)

const (
	helpAskQuestionURL = "https://x.com/ml_review"
	helpFileIssueURL   = "https://github.com/swobuforge/swobu/issues"
)

func BuildHelpSection(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	note := model.HelpNote
	rows := []view.ViewSpec[state.Model]{
		helpActionRow("ask question", helpAskQuestionURL, note),
		helpActionRow("file issue", helpFileIssueURL, note),
	}
	return Section("help & feedback", rows...)
}

func helpActionRow(label string, url string, note string) view.ViewSpec[state.Model] {
	row := RowActionWithHooks(label, "", "open", func() []update.Action {
		return []update.Action{state.OpenSupportLinkRequested{Label: label, URL: url}}
	}, nil, focusAffordance("open", false))
	if fallbackURL := fallbackURLForHelpAction(note, label); fallbackURL != "" {
		noteRows := DisclosureNoteRows(fallbackURL)
		return anchoredDisclosureWithScrollableDetails(row, 4, 0, false, false, noteRows...)
	}
	return row
}

func fallbackURLForHelpAction(note string, label string) string {
	note = strings.TrimSpace(note)
	label = strings.TrimSpace(label)
	if note == "" || label == "" {
		return ""
	}
	prefixes := []string{
		label + " opened; fallback ",
		label + " open failed; fallback ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(note, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(note, prefix))
		}
	}
	return ""
}
