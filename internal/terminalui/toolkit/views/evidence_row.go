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
	marker := strings.TrimSpace(spec.Marker) // trimlowerlint:allow boundary canonicalization
	if marker == "" {
		marker = " "
	}
	base := fmt.Sprintf(
		"%s   %-8s %-11s %-19s %7s   %-8s",
		marker,
		trimToWidth(strings.TrimSpace(spec.Time), 8),   // trimlowerlint:allow boundary canonicalization
		trimToWidth(strings.TrimSpace(spec.Kind), 11),  // trimlowerlint:allow boundary canonicalization
		trimToWidth(strings.TrimSpace(spec.Route), 19), // trimlowerlint:allow boundary canonicalization
		trimToWidth(strings.TrimSpace(spec.Timing), 7), // trimlowerlint:allow boundary canonicalization
		trimToWidth(strings.TrimSpace(spec.Result), 8), // trimlowerlint:allow boundary canonicalization
	)
	if action := strings.TrimSpace(spec.Action); action != "" { // trimlowerlint:allow boundary canonicalization
		base += " " + action
	}
	return padRight(trimToWidth(base, width), width)
}
