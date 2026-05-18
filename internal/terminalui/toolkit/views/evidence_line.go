package views

import (
	"fmt"
	"strings"
)

// EvidenceRowSpec defines one evidence row line.
type EvidenceRowSpec struct {
	Marker string
	Time   string
	Kind   string
	Route  string
	Timing string
	Result string
	Action string
}

// RenderEvidenceRow renders one fixed-column evidence row with optional action.
func RenderEvidenceRow(width int, spec EvidenceRowSpec) string {
	if width <= 0 {
		return ""
	}
	marker := strings.TrimSpace(spec.Marker) // swobu:io-string source=boundary
	if marker == "" {
		marker = " "
	}
	base := fmt.Sprintf(
		"%s   %-8s %-11s %-19s %7s   %-8s",
		marker,
		trimToWidth(strings.TrimSpace(spec.Time), 8),   // swobu:io-string source=boundary
		trimToWidth(strings.TrimSpace(spec.Kind), 11),  // swobu:io-string source=boundary
		trimToWidth(strings.TrimSpace(spec.Route), 19), // swobu:io-string source=boundary
		trimToWidth(strings.TrimSpace(spec.Timing), 7), // swobu:io-string source=boundary
		trimToWidth(strings.TrimSpace(spec.Result), 8), // swobu:io-string source=boundary
	)
	if action := strings.TrimSpace(spec.Action); action != "" { // swobu:io-string source=boundary
		base += " " + action
	}
	return padRight(trimToWidth(base, width), width)
}
