package compat

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// Issue identifies one canonical semantic occurrence that a concrete lowering
// could not preserve. It is candidate-local and does not choose recovery.
type Issue struct {
	capability canonical.CapabilityPath
	occurrence canonical.Occurrence
}

func NewIssue(capability canonical.CapabilityPath, occurrence canonical.Occurrence) Issue {
	if capability == "" {
		panic("compatibility issue requires a capability")
	}
	return Issue{capability: capability, occurrence: occurrence}
}

func (i Issue) Capability() canonical.CapabilityPath { return i.capability }
func (i Issue) Occurrence() canonical.Occurrence     { return i.occurrence }

// UnsupportedError is a typed lowering failure. It never means that every route
// candidate is unsupported; exchange owns that terminal conclusion.
type UnsupportedError struct {
	issues []Issue
}

func NewUnsupported(issues ...Issue) UnsupportedError {
	if len(issues) == 0 {
		panic("compatibility unsupported failure requires at least one issue")
	}
	return UnsupportedError{issues: append([]Issue(nil), issues...)}
}

func (e UnsupportedError) Error() string {
	paths := make([]string, 0, len(e.issues))
	for _, issue := range e.issues {
		paths = append(paths, issue.capability.String())
	}
	return fmt.Sprintf("canonical semantics cannot be lowered: %s", strings.Join(paths, ", "))
}

func (e UnsupportedError) Issues() []Issue { return append([]Issue(nil), e.issues...) }
