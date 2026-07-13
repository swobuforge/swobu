package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/components/compound"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/corelower"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

const (
	helpAskQuestionURL = "https://x.com/ml_review"
	helpFileIssueURL   = "https://github.com/swobuforge/swobu/issues"
)

// BuildHelpSectionNode returns the help section as a pure semantic core.Node.
// This is the canonical core-algebra path for the help screen.
func BuildHelpSectionNode(model state.Model) core.Node[state.Action] {
	note := model.HelpNote
	rows := []core.Node[state.Action]{
		helpActionNode("ask question", helpAskQuestionURL, note),
		helpActionNode("file issue", helpFileIssueURL, note),
	}
	return compound.SectionNode[state.Action]("help & feedback", rows...)
}

// BuildHelpSection is the retained bridge wrapper.
// It lowers the canonical core node into a retained ViewSpec for composition
// during the migration period.  New code should prefer BuildHelpSectionNode.
func BuildHelpSection(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	return retained.View[state.Model](func(ctx *retained.Context[state.Model]) retained.RenderNode {
		model := ctx.Model()
		node := BuildHelpSectionNode(model)
		renderNode, err := corelower.Lower(node, corelower.EnvConfig{DevMode: true}, func(a state.Action) update.Action {
			return a
		})
		if err != nil {
			return nil
		}
		return renderNode
	})
}

func helpActionNode(label string, url string, note string) core.Node[state.Action] {
	row := compound.SettingRow(compound.SettingRowProps[state.Action]{
		Key:         core.K("help/" + strings.ReplaceAll(label, " ", "-")),
		Label:       label,
		Value:       "",
		ActionLabel: "open ↵",
		Signal: core.SignalEvent[state.Action]{
			Kind:  cockpitActionSignalKind,
			Event: state.OpenSupportLinkRequested{Label: label, URL: url},
		},
		FocusSignal: core.SignalEvent[state.Action]{
			Kind:  cockpitRowFocusSignalKind,
			Event: state.SetFocusedRowAffordance{Verb: "open", AllowSpace: false},
		},
		Help: []core.HelpBindingSpec{{Key: "↵", Label: "open"}},
	})

	if fallbackURL := fallbackURLForHelpAction(note, label); fallbackURL != "" {
		// TODO: text wrapping for disclosure notes requires toolkit support.
		// For now, append raw fallback URL on a second line.
		return core.Box[state.Action](
			row,
			core.Text[state.Action]("  -> "+fallbackURL).
				Style(core.Style{Token: core.TokenTextMuted, State: core.StateDefault}),
		)
	}
	return row
}

func fallbackURLForHelpAction(note string, label string) string {
	note = strings.TrimSpace(note)   // swobu:io-string source=boundary
	label = strings.TrimSpace(label) // swobu:io-string source=boundary
	if note == "" || label == "" {
		return ""
	}
	prefixes := []string{
		label + " opened; fallback ",
		label + " open failed; fallback ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(note, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(note, prefix)) // swobu:io-string source=boundary
		}
	}
	return ""
}
