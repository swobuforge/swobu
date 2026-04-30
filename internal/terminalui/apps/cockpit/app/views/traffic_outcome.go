package views

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
	toolkitviews "github.com/metrofun/swobu/internal/terminalui/toolkit/views"
)

func trafficOutcome(row state.TrafficRow) string {
	switch trafficResult(row) {
	case "done":
		return "ok"
	case "inflight":
		return "live"
	default:
		return "failed"
	}
}

func trafficFailureOwner(row state.TrafficRow) string {
	if trafficResult(row) == "done" || trafficResult(row) == "inflight" {
		return "n/a"
	}
	return errorOrigin(row)
}

func trafficHTTPStatus(row state.TrafficRow) string {
	if row.StatusCode <= 0 {
		return "n/a"
	}
	return strconv.Itoa(row.StatusCode)
}

func normalizeTrafficRows(rows []state.TrafficRow) []state.TrafficRow {
	out := append([]state.TrafficRow(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool {
		leftTime := strings.TrimSpace(out[i].ObservedAt)
		rightTime := strings.TrimSpace(out[j].ObservedAt)
		if leftTime != rightTime {
			return leftTime > rightTime
		}
		leftRank := trafficOutcomeRank(out[i])
		rightRank := trafficOutcomeRank(out[j])
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		left := strings.TrimSpace(out[i].Target)
		right := strings.TrimSpace(out[j].Target)
		if left != right {
			return left < right
		}
		return strings.TrimSpace(out[i].OperationFamily) < strings.TrimSpace(out[j].OperationFamily)
	})
	return out
}

func trafficOutcomeRank(row state.TrafficRow) int {
	switch trafficOutcome(row) {
	case "failed":
		return 0
	case "live":
		return 1
	default:
		return 2
	}
}

func buildTrafficRowKeys(rows []state.TrafficRow) []string {
	keys := make([]string, len(rows))
	seen := make(map[string]int, len(rows))
	for idx, row := range rows {
		base := trafficRowKeyBase(row)
		seen[base]++
		if seen[base] == 1 {
			keys[idx] = base
			continue
		}
		keys[idx] = fmt.Sprintf("%s-%d", base, seen[base])
	}
	return keys
}

func trafficRowKeyBase(row state.TrafficRow) string {
	base := trafficKeyToken(row.RequestID)
	if base == "" {
		base = trafficKeyToken(row.ObservedAt)
	}
	if base == "" {
		base = "row"
	}
	return base
}

func trafficKeyToken(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var out strings.Builder
	out.Grow(len(value))
	lastUnderscore := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			out.WriteRune(r)
			lastUnderscore = false
		case r == '-' || r == '.':
			out.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				out.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(out.String(), "_")
}

func trafficResult(row state.TrafficRow) string {
	result := strings.ToLower(strings.TrimSpace(row.Result))
	if result == "in_progress" || result == "inflight" || result == "in flight" || row.StatusCode == 0 {
		return "inflight"
	}
	if row.StatusCode == http.StatusTooManyRequests {
		return "429"
	}
	if strings.Contains(result, "unsupported_endpoint") {
		return "ERR"
	}
	if row.StatusCode >= 400 {
		return strconv.Itoa(row.StatusCode)
	}
	if row.StatusCode >= 200 && row.StatusCode < 300 {
		return "done"
	}
	if result == "success" || result == "ok" {
		return "done"
	}
	return strings.ToUpper(toolkitviews.TrimToWidth(strings.TrimSpace(row.Result), 8))
}

func trafficKind(row state.TrafficRow) string {
	op := strings.ToLower(strings.TrimSpace(row.OperationFamily))
	if strings.Contains(op, "response") {
		return "responses"
	}
	if strings.Contains(op, "chat") {
		return "chat"
	}
	return "responses"
}

func trafficTiming(row state.TrafficRow) string {
	if row.DurMillis != nil {
		return fmt.Sprintf("%d ms", *row.DurMillis)
	}
	if row.TTFBMillis != nil {
		return fmt.Sprintf("%d ms", *row.TTFBMillis)
	}
	return "0 ms"
}

func trafficWhen(row state.TrafficRow) string {
	when := strings.TrimSpace(row.ObservedAt)
	if when != "" {
		return when
	}
	return "........"
}
