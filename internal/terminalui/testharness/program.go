package testharness

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
)

// Session is the minimal loop-kind runtime contract for cockpit interaction tests.
// It intentionally avoids root-specific nouns.
type Session struct {
	Render  func() string
	SendKey func(interaction.Key)
}

// Condition evaluates against the current rendered surface.
type Condition struct {
	Name string
	Eval func(out string) bool
}

// Step is one executable harness step.
type Step interface {
	run(*Session) error
	describe() string
}

// Program is an ordered sequence of steps.
type Program []Step

// Plan builds a runnable program.
func Plan(steps ...Step) Program { return Program(steps) }

// Run executes a program and returns the first failure with step context.
func Run(s *Session, p Program) error {
	for i, step := range p {
		if err := step.run(s); err != nil {
			return fmt.Errorf("step %d (%s): %w", i+1, step.describe(), err)
		}
	}
	return nil
}

type doStep struct {
	key    interaction.Key
	until  Condition
	within int
}

func (d doStep) run(s *Session) error {
	for i := 0; i < d.within; i++ {
		s.SendKey(d.key)
		if d.until.Eval(s.Render()) {
			return nil
		}
	}
	return fmt.Errorf("condition %q not satisfied after %d actions; render=%q", d.until.Name, d.within, s.Render())
}

func (d doStep) describe() string {
	return fmt.Sprintf("Do(%s) until %s", d.key.String(), d.until.Name)
}

type doBuilder struct{ key interaction.Key }

// Do starts an action step.
func Do(key interaction.Key) doBuilder { return doBuilder{key: key} }

// Until finalizes a do step with success condition.
func (b doBuilder) Until(c Condition) doUntilBuilder { return doUntilBuilder{key: b.key, until: c} }

type doUntilBuilder struct {
	key   interaction.Key
	until Condition
}

// Within sets max action attempts.
func (b doUntilBuilder) Within(n int) Step {
	if n <= 0 {
		n = 1
	}
	return doStep{key: b.key, until: b.until, within: n}
}

type ensureStep struct {
	cond       Condition
	eventually int
}

func (e ensureStep) run(s *Session) error {
	for i := 0; i < e.eventually; i++ {
		if e.cond.Eval(s.Render()) {
			return nil
		}
	}
	return fmt.Errorf("condition %q not satisfied after %d checks; render=%q", e.cond.Name, e.eventually, s.Render())
}

func (e ensureStep) describe() string { return fmt.Sprintf("Ensure(%s)", e.cond.Name) }

type ensureBuilder struct{ cond Condition }

// Ensure starts a pure assertion step.
func Ensure(c Condition) ensureBuilder { return ensureBuilder{cond: c} }

// Eventually sets max polling checks.
func (b ensureBuilder) Eventually(n int) Step {
	if n <= 0 {
		n = 1
	}
	return ensureStep{cond: b.cond, eventually: n}
}

// TextContains matches a substring.
func TextContains(text string) Condition {
	trimmed := strings.TrimSpace(text) // swobu:io-string source=boundary
	return Condition{
		Name: fmt.Sprintf("TextContains(%q)", trimmed),
		Eval: func(out string) bool { return strings.Contains(out, trimmed) },
	}
}

// FocusedRowContains matches when focused line contains token.
func FocusedRowContains(token string) Condition {
	trimmed := strings.TrimSpace(token) // swobu:io-string source=boundary
	return Condition{
		Name: fmt.Sprintf("FocusedRowContains(%q)", trimmed),
		Eval: func(out string) bool { return FocusedLineContains(out, trimmed) },
	}
}
