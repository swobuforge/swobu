package views

import (
	"strconv"
	"strings"

	"github.com/metrofun/swobu/internal/app/operator/clientprofile"
	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/metrofun/swobu/internal/terminalui/toolkit/views"
)

func contentRows(content string) []view.ViewSpec[state.Model] {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	rows := make([]view.ViewSpec[state.Model], 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows = append(rows, wrappedPayloadTextRows(line)...)
	}
	return rows
}

func anchoredDisclosureWithScrollableDetails(
	parent view.ViewSpec[state.Model],
	maxHeight int,
	offset int,
	showMoreAbove bool,
	showMoreBelow bool,
	details ...view.ViewSpec[state.Model],
) view.ViewSpec[state.Model] {
	if maxHeight <= 0 {
		maxHeight = 8
	}
	filtered := make([]view.ViewSpec[state.Model], 0, len(details))
	for _, detail := range details {
		if detail == nil {
			continue
		}
		filtered = append(filtered, detail)
	}
	if len(filtered) == 0 {
		return parent
	}
	detailStack := view.VStack[state.Model](nil, filtered...)
	detailViewport := view.WithConstrain[state.Model](view.ConstrainSpec{
		GrowW: true,
		MaxW:  ContentMaxWidth,
		MaxH:  maxHeight,
	})(view.WithScrollY[state.Model](offset)(detailStack))
	out := make([]view.ViewSpec[state.Model], 0, 2)
	if cue := disclosureScrollCue(showMoreAbove, showMoreBelow); cue != "" {
		out = append(out, payloadTextRow(cue))
	}
	out = append(out, detailViewport)
	return toolkitviews.NewAnchoredDisclosure(parent, out...)
}

func disclosureScrollCue(showMoreAbove bool, showMoreBelow bool) string {
	if showMoreAbove && showMoreBelow {
		return "↑ more  ·  ↓ more"
	}
	if showMoreAbove {
		return "↑ more"
	}
	if showMoreBelow {
		return "↓ more"
	}
	return ""
}

func keyScopeForDisclosureScroll(
	disclosure view.ViewSpec[state.Model],
	local clientsSectionState,
	maxOffset int,
) view.ViewSpec[state.Model] {
	if maxOffset == 0 {
		return disclosure
	}
	return toolkitviews.KeyScope(disclosure, func(_ *view.Context[state.Model], ev interaction.Event) (bool, []update.Action) {
		if ev.Kind != interaction.EventKey {
			return false, nil
		}
		switch ev.Key {
		case interaction.KeyDown:
			if local.payloadScrollOffset >= maxOffset {
				return false, nil
			}
			local.setPayloadScrollOffset(local.payloadScrollOffset + 1)
			return true, nil
		case interaction.KeyUp:
			if local.payloadScrollOffset <= 0 {
				return false, nil
			}
			local.setPayloadScrollOffset(local.payloadScrollOffset - 1)
			return true, nil
		default:
			return false, nil
		}
	})
}

func payloadMaxOffset(rowCount int, maxHeight int) int {
	maxOffset := rowCount - maxHeight
	if maxOffset < 0 {
		return 0
	}
	return maxOffset
}

func actionStableID(action clientprofile.Action) string {
	if strings.TrimSpace(action.ID) != "" {
		return strings.TrimSpace(action.ID)
	}
	if action.RowLabel() != "" {
		return action.RowLabel()
	}
	return "action"
}

func clientPickerFocusKey(index int) string {
	if index < 0 {
		index = 0
	}
	return "client-option/" + strconv.Itoa(index)
}
