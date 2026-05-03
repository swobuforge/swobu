package views

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func collapsedTrafficSummary(model state.Model) string {
	if model.TrafficError != "" {
		return "stale snapshot"
	}
	if len(model.TrafficRows) == 0 {
		return "no traffic yet"
	}
	metrics := summarizeTrafficUsageMetrics(model.TrafficRows)
	base := fmt.Sprintf("%d req · ok %s · p95 %s", len(model.TrafficRows), metrics.okRateLabel(), trafficP95(model.TrafficRows))
	if !metrics.hasCoverage() {
		return base + " · usage unknown"
	}
	return fmt.Sprintf(
		"%s · cache %s (coverage %d%%) · in %s / out %s",
		base,
		metrics.cacheReadRateLabel(),
		metrics.coveragePercent(),
		compactTokenCount(metrics.sumInputTokens),
		compactTokenCount(metrics.sumOutputTokens),
	)
}

type trafficUsageMetrics struct {
	terminalCount   int
	okCount         int
	coveredCount    int
	sumInputTokens  int
	sumOutputTokens int
	sumCacheRead    int
}

func (summary trafficUsageMetrics) hasCoverage() bool {
	return summary.coveredCount > 0 && summary.terminalCount > 0
}

func (summary trafficUsageMetrics) coveragePercent() int {
	if summary.terminalCount <= 0 {
		return 0
	}
	return percentage(summary.coveredCount, summary.terminalCount)
}

func (summary trafficUsageMetrics) cacheReadRateLabel() string {
	if summary.sumInputTokens <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d%%", percentage(summary.sumCacheRead, summary.sumInputTokens))
}

func (summary trafficUsageMetrics) okRateLabel() string {
	if summary.terminalCount <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", percentage(summary.okCount, summary.terminalCount))
}

func summarizeTrafficUsageMetrics(rows []state.TrafficRow) trafficUsageMetrics {
	out := trafficUsageMetrics{}
	for _, row := range rows {
		result := trafficResult(row)
		if result == "inflight" {
			continue
		}
		out.terminalCount++
		if result == "done" {
			out.okCount++
		}
		if row.InputTokens == nil || row.OutputTokens == nil || row.CacheReadTokens == nil {
			continue
		}
		out.coveredCount++
		out.sumInputTokens += *row.InputTokens
		out.sumOutputTokens += *row.OutputTokens
		out.sumCacheRead += *row.CacheReadTokens
	}
	return out
}

func trafficP95(rows []state.TrafficRow) string {
	values := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.DurMillis != nil {
			values = append(values, *row.DurMillis)
		}
	}
	if len(values) == 0 {
		return "n/a"
	}
	sort.Ints(values)
	idx := int(float64(len(values)-1) * 0.95)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(values) {
		idx = len(values) - 1
	}
	return fmt.Sprintf("%d ms", values[idx])
}

func percentage(part int, total int) int {
	if part <= 0 || total <= 0 {
		return 0
	}
	return (part * 100) / total
}

func compactTokenCount(value int) string {
	if value < 0 {
		value = 0
	}
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%dm", value/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%dk", value/1_000)
	default:
		return strconv.Itoa(value)
	}
}
