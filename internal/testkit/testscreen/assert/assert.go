package screenassert

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/testkit/testscreen/tempo"
)

// Predicate is a boolean condition over rendered text.
type Predicate interface {
	eval(string) bool
	String() string
}

// TextExpr is one literal or regular-expression text query.
type TextExpr struct {
	description string
	match       func(string) bool
}

func Text(needle string) TextExpr {
	return TextExpr{description: fmt.Sprintf("Text(%q)", needle), match: func(screen string) bool {
		if strings.TrimSpace(needle) == "" {
			return false
		}
		for _, line := range strings.Split(screen, "\n") {
			if strings.Contains(line, needle) {
				return true
			}
		}
		return false
	}}
}
func TextRE(pattern string) TextExpr {
	re := regexp.MustCompile(pattern)
	return TextExpr{description: fmt.Sprintf("TextRE(%q)", pattern), match: func(screen string) bool {
		if strings.TrimSpace(pattern) == "" {
			return false
		}
		for _, line := range strings.Split(screen, "\n") {
			if re.MatchString(line) {
				return true
			}
		}
		return false
	}}
}
func (e TextExpr) Exists() Predicate {
	return textPredicate{description: fmt.Sprintf("%s.Exists()", e.String()), match: e.match}
}
func (e TextExpr) String() string { return e.description }

type textPredicate struct {
	description string
	match       func(string) bool
}

func (p textPredicate) eval(screen string) bool { return p.match != nil && p.match(screen) }
func (p textPredicate) String() string          { return p.description }

type allPredicate struct{ children []Predicate }
type notPredicate struct{ child Predicate }

func All(predicates ...Predicate) Predicate { return allPredicate{children: predicates} }
func Not(predicate Predicate) Predicate     { return notPredicate{child: predicate} }

func (p allPredicate) eval(screen string) bool {
	for _, child := range p.children {
		if child == nil || !child.eval(screen) {
			return false
		}
	}
	return true
}
func (p allPredicate) String() string {
	parts := make([]string, 0, len(p.children))
	for _, child := range p.children {
		if child == nil {
			parts = append(parts, "<nil>")
		} else {
			parts = append(parts, child.String())
		}
	}
	return fmt.Sprintf("All(%s)", strings.Join(parts, ", "))
}
func (p notPredicate) eval(screen string) bool { return p.child != nil && !p.child.eval(screen) }
func (p notPredicate) String() string {
	if p.child == nil {
		return "Not(<nil>)"
	}
	return fmt.Sprintf("Not(%s)", p.child.String())
}

func EvalNow(screen string, predicate Predicate) error {
	if predicate == nil {
		return fmt.Errorf("predicate is required")
	}
	if predicate.eval(screen) {
		return nil
	}
	return fmt.Errorf("predicate failed now: %s", predicate.String())
}

func EvalEventually(timeout time.Duration, snapshot func() string, predicate Predicate) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is required")
	}
	if predicate == nil {
		return fmt.Errorf("predicate is required")
	}
	err := tempo.Eventually(timeout, 50*time.Millisecond, func() bool {
		return predicate.eval(snapshot())
	})
	if err != nil {
		return fmt.Errorf("predicate failed within %s: %s", timeout, predicate.String())
	}
	return nil
}
