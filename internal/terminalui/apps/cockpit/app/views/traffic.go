// Traffic section view.
package views

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
)

const trafficVisibleWindow = 5

// BuildTrafficSection composes the traffic section rows.
func BuildTrafficSection(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	offset, setOffset := view.UseState(ctx, func() int { return 0 })
	var section view.ViewSpec[state.Model]
	if model.CurrentEndpoint == "" {
		if model.InteractionMode == state.InteractionModeBusySave {
			section = staticSectionSummary(ctx, SectionTraffic, "empty")
		} else {
			section = NewCollapsibleSection(
				SectionTraffic,
				false,
				"open",
				SummaryRow("empty"),
			)
		}
	} else {
		summary := collapsedTrafficSummary(model)
		if model.HeaderStatus == "saved" {
			summary = "no traffic yet"
			section = staticSectionSummary(ctx, SectionTraffic, summary)
		} else {
			body := make([]view.ViewSpec[state.Model], 0, len(model.TrafficRows)+3)
			if model.TrafficError != "" {
				body = append(body, view.Named[state.Model]("error", RowStatic("", "-> "+model.TrafficError)))
			} else if len(model.TrafficRows) == 0 {
				body = append(body, view.Named[state.Model]("empty", SummaryRow("no traffic yet")))
			} else {
				rows := normalizeTrafficRows(model.TrafficRows)
				rowKeys := buildTrafficRowKeys(rows)
				start, end := ListWindowBounds(len(rows), offset, trafficVisibleWindow)
				visibleRows := rows[start:end]
				visibleKeys := rowKeys[start:end]
				children := make([]view.ViewSpec[state.Model], 0, len(visibleRows))
				for idx, row := range visibleRows {
					absIdx := start + idx
					key := fmt.Sprintf("%s-%d", visibleKeys[idx], absIdx)
					rowItem := row
					isFirstVisible := idx == 0
					isLastVisible := idx == len(visibleRows)-1
					children = append(children, view.Named[state.Model]("traffic/"+key, view.Build[state.Model](func(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
						base := trafficRow(ctx, rowItem, func() []update.Action {
							return []update.Action{state.SetFocusedRowAffordance{Verb: "open"}}
						})
						return toolkitviews.KeyScope(base, func(_ *view.Context[state.Model], ev interaction.Event) (bool, []update.Action) {
							if ev.Kind != interaction.EventKey {
								return false, nil
							}
							switch ev.Key {
							case interaction.KeyDown:
								if !isLastVisible || end >= len(rows) {
									return false, nil
								}
								maxOffset := len(rows) - trafficVisibleWindow
								if maxOffset < 0 {
									maxOffset = 0
								}
								nextOffset := offset + 1
								if nextOffset > maxOffset {
									nextOffset = maxOffset
								}
								if nextOffset == offset {
									return true, nil
								}
								setOffset(nextOffset)
								return true, []update.Action{state.FocusNextAfterRebuildRequested{}}
							case interaction.KeyUp:
								if !isFirstVisible || start == 0 {
									return false, nil
								}
								prevOffset := offset - 1
								if prevOffset < 0 {
									prevOffset = 0
								}
								if prevOffset == offset {
									return true, nil
								}
								setOffset(prevOffset)
								return true, nil
							default:
								return false, nil
							}
						})
					})))
				}
				list := view.VStack(ctx, children...)
				body = append(body, list)
			}
			section = NewCollapsibleSection(
				SectionTraffic,
				false,
				"open",
				SummaryRow(summary),
				body...,
			)
		}
	}
	return section
}

func trafficRow(ctx *view.Context[state.Model], row state.TrafficRow, onFocus func() []update.Action) view.ViewSpec[state.Model] {
	open, setOpen := view.UseState(ctx, func() bool { return false })
	parent := toolkitviews.NewAction(64, true, false, func(focused bool, width int) string {
		return renderTrafficListRow(width, focused, row, open)
	}, func(string) []update.Action {
		nextOpen := !open
		setOpen(nextOpen)
		verb := "open"
		if nextOpen {
			verb = "close"
		}
		return []update.Action{state.SetFocusedRowAffordance{Verb: verb}}
	}, func() []update.Action {
		if !open {
			return nil
		}
		setOpen(false)
		return []update.Action{state.SetFocusedRowAffordance{Verb: "open"}}
	})
	parent.OnFocusAction = onFocus
	var out view.ViewSpec[state.Model] = view.FromRenderNode[state.Model](parent)
	if open {
		out = toolkitviews.NewAnchoredDisclosure(out, trafficOpenDetailRows(row)...)
	}
	return out
}

func renderTrafficListRow(width int, focused bool, row state.TrafficRow, open bool) string {
	marker := " "
	if focused {
		marker = ">"
	}
	outcome := trafficOutcome(row)
	usage := deriveTrafficUsageStats(row)
	action := ""
	if focused {
		action = "open ↵"
		if open {
			action = "close ↵"
		}
	}
	return toolkitviews.RenderEvidenceRow(width, toolkitviews.EvidenceRowSpec{
		Marker: marker,
		Time:   trafficWhen(row),
		Kind:   strings.TrimSpace(outcome + " " + trafficKind(row)),
		Route:  trafficBurnSummary(usage),
		Timing: trafficTiming(row),
		Result: trafficCacheSummary(row),
		Action: action,
	})
}

func trafficOpenDetailRows(row state.TrafficRow) []view.ViewSpec[state.Model] {
	rows := []view.ViewSpec[state.Model]{
		trafficDetailLine("request id", strings.TrimSpace(row.RequestID)),
		trafficDetailLine("outcome", trafficOutcome(row)),
		trafficDetailLine("family", trafficKind(row)),
		trafficDetailLine("owner", trafficFailureOwner(row)),
		trafficDetailLine("route", strings.TrimSpace(row.Target)),
		trafficDetailLine("http", trafficHTTPStatus(row)),
		trafficDetailLine("ttfb", previewTTFB(row)),
		trafficDetailLine("duration", previewDuration(row)),
	}
	return append(rows, trafficTokenDetailLines(row)...)
}

func trafficDetailLine(label string, value string) view.ViewSpec[state.Model] {
	line := toolkitviews.FormatKeyValueTextLine(strings.TrimSpace(label), strings.TrimSpace(value), 24)
	return IndentLeft[state.Model](StaticTextLine[state.Model](line), InsetDetail)
}

func previewDuration(row state.TrafficRow) string {
	if row.DurMillis != nil {
		return fmt.Sprintf("%d ms", *row.DurMillis)
	}
	if row.TTFBMillis != nil {
		return fmt.Sprintf("%d ms", *row.TTFBMillis)
	}
	return "0 ms"
}

func previewTTFB(row state.TrafficRow) string {
	if row.TTFBMillis != nil {
		return fmt.Sprintf("%d ms", *row.TTFBMillis)
	}
	return "n/a"
}

func errorOrigin(row state.TrafficRow) string {
	if row.StatusCode == 0 {
		return "swobu"
	}
	return "backend"
}

func trafficTokenDetailLines(row state.TrafficRow) []view.ViewSpec[state.Model] {
	usage := deriveTrafficUsageStats(row)
	out := make([]view.ViewSpec[state.Model], 0, 6)
	if usage.hasCoverage() {
		out = append(out, trafficDetailLine("usage", fmt.Sprintf("in %s · out %s", compactTokenCount(usage.input), compactTokenCount(usage.output))))
		out = append(out, trafficDetailLine("cache", fmt.Sprintf("read %s / in %s · %s · write %s", compactTokenCount(usage.cacheRead), compactTokenCount(usage.input), trafficCacheSummary(row), compactTokenCount(usage.cacheWrite))))
		out = append(out, trafficDetailLine("coverage", "100%"))
		return out
	}
	out = append(out, trafficDetailLine("usage", "unknown"))
	out = append(out, trafficDetailLine("cache", "unknown"))
	out = append(out, trafficDetailLine("coverage", "0%"))
	return out
}

type trafficUsageStats struct {
	input      int
	output     int
	cacheRead  int
	cacheWrite int
	hasInput   bool
	hasOutput  bool
	hasCache   bool
}

func deriveTrafficUsageStats(row state.TrafficRow) trafficUsageStats {
	stats := trafficUsageStats{}
	if row.InputTokens != nil {
		stats.input = *row.InputTokens
		stats.hasInput = true
	}
	if row.OutputTokens != nil {
		stats.output = *row.OutputTokens
		stats.hasOutput = true
	}
	if row.CacheReadTokens != nil {
		stats.cacheRead = *row.CacheReadTokens
		stats.hasCache = true
	}
	if row.CacheWriteTokens != nil {
		stats.cacheWrite = *row.CacheWriteTokens
	}
	return stats
}

func (stats trafficUsageStats) hasCoverage() bool {
	return stats.hasInput && stats.hasOutput && stats.hasCache
}

func trafficBurnSummary(usage trafficUsageStats) string {
	if !usage.hasCoverage() {
		return "usage unknown"
	}
	return fmt.Sprintf("in %s / out %s", compactTokenCount(usage.input), compactTokenCount(usage.output))
}

func trafficCacheSummary(row state.TrafficRow) string {
	usage := deriveTrafficUsageStats(row)
	if !usage.hasCoverage() || usage.input <= 0 {
		return "c n/a"
	}
	return fmt.Sprintf("c %d%%", percentage(usage.cacheRead, usage.input))
}
