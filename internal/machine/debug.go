package machine

import (
	"fmt"
	"strings"
)

// PrintPlan writes a human-readable trace of the machine's execution.
// It is used for debug output and is deterministic in tests.
func PrintPlan(plan []PlanStep) string {
	var b strings.Builder
	for i, step := range plan {
		b.WriteString(fmt.Sprintf("step %d: event=%T\n", i+1, step.Event))
		for _, name := range step.Reducers {
			b.WriteString(fmt.Sprintf("  reducer: %s\n", name))
		}
		for _, name := range step.Commands {
			b.WriteString(fmt.Sprintf("  command: %s\n", name))
		}
	}
	return b.String()
}
