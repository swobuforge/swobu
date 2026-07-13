// Production disclosure helpers for clients section (retained bridge).
package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/app/operator/clientprofile"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func anchoredDisclosureWithScrollableDetails(
	parent retained.ViewSpec[state.Model],
	maxHeight int,
	offset int,
	showMoreAbove bool,
	showMoreBelow bool,
	details ...retained.ViewSpec[state.Model],
) retained.ViewSpec[state.Model] {
	if maxHeight <= 0 {
		maxHeight = 8
	}
	filtered := make([]retained.ViewSpec[state.Model], 0, len(details))
	for _, detail := range details {
		if detail == nil {
			continue
		}
		filtered = append(filtered, detail)
	}
	if len(filtered) == 0 {
		return parent
	}
	detailStack := retained.VStack[state.Model](nil, filtered...)
	detailViewport := retained.Constrain[state.Model](
		retained.ScrollY[state.Model](detailStack, offset),
		retained.ConstrainSpec{
			GrowW: true,
			MaxW:  ContentMaxWidth,
			MaxH:  maxHeight,
		},
	)
	out := make([]retained.ViewSpec[state.Model], 0, 2)
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
	disclosure retained.ViewSpec[state.Model],
	model state.Model,
	maxOffset int,
) retained.ViewSpec[state.Model] {
	if maxOffset == 0 {
		return disclosure
	}
	return toolkitviews.KeyScope(disclosure, func(_ *retained.Context[state.Model], ev interaction.Event) (bool, []update.Action) {
		if ev.Kind != interaction.EventKey {
			return false, nil
		}
		switch ev.Key {
		case interaction.KeyDown:
			if model.PayloadScrollOffset >= maxOffset {
				return false, nil
			}
			return true, []update.Action{state.SetPayloadScrollOffset{Offset: model.PayloadScrollOffset + 1}}
		case interaction.KeyUp:
			if model.PayloadScrollOffset <= 0 {
				return false, nil
			}
			return true, []update.Action{state.SetPayloadScrollOffset{Offset: model.PayloadScrollOffset - 1}}
		default:
			return false, nil
		}
	})
}

func payloadMaxOffsetDisclosure(rowCount int, maxHeight int) int {
	maxOffset := rowCount - maxHeight
	if maxOffset < 0 {
		return 0
	}
	return maxOffset
}

func actionStableIDDisclosure(action clientprofile.Action) string {
	id := strings.TrimSpace(action.ID) // swobu:io-string source=boundary
	if id != "" {
		return id
	}
	if action.RowLabel() != "" {
		return action.RowLabel()
	}
	return "action"
}

func clientPickerFocusKeyOld(profile clientprofile.Profile) string {
	id := ""
	if profile != nil {
		identity := profile.Identity()
		id = strings.TrimSpace(identity.ID) // swobu:io-string source=boundary
		if id == "" {
			id = strings.TrimSpace(identity.Label) // swobu:io-string source=boundary
		}
	}
	if id == "" {
		id = "client"
	}
	return "client-option/" + id
}
