package screenassert

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/swobuforge/swobu/testscreen/buf"
	"github.com/swobuforge/swobu/testscreen/tempo"
)

// Expr is a hierarchical expression over rendered text matches.
type Expr interface {
	eval(frame Frame) []matchBox
	LeftOf(other Expr) Expr
	RightOf(other Expr) Expr
	Below(other Expr) Expr
	Above(other Expr) Expr
	Near(other Expr) Expr
	Exists() Predicate
	String() string
}

// Predicate is a boolean condition over a frame.
type Predicate interface {
	eval(frame Frame) bool
	String() string
}

type Frame struct{ Lines []string }

type matchBox struct{ X, Y, W int }

func Parse(screen string) Frame { return ParseView(buf.FromString(screen)) }

func ParseView(view buf.View) Frame {
	if view == nil {
		return Frame{}
	}
	_, rows := view.Size()
	lines := make([]string, 0, rows)
	for y := 0; y < rows; y++ {
		lines = append(lines, view.Line(y))
	}
	return Frame{Lines: lines}
}

type textExpr struct{ needle string }

func Text(needle string) Expr { return textExpr{needle: needle} }

type regexExpr struct {
	pattern string
	re      *regexp.Regexp
}

func TextRE(pattern string) Expr { return regexExpr{pattern: pattern, re: regexp.MustCompile(pattern)} }

func (e textExpr) eval(frame Frame) []matchBox {
	if strings.TrimSpace(e.needle) == "" {
		return nil
	}
	var out []matchBox
	for y, line := range frame.Lines {
		start := 0
		for {
			i := strings.Index(line[start:], e.needle)
			if i < 0 {
				break
			}
			x := start + i
			out = append(out, matchBox{X: x, Y: y, W: len(e.needle)})
			start = x + 1
			if start >= len(line) {
				break
			}
		}
	}
	return out
}
func (e textExpr) LeftOf(other Expr) Expr {
	return relationExpr{name: "left_of", left: e, right: other}
}
func (e textExpr) RightOf(other Expr) Expr {
	return relationExpr{name: "right_of", left: e, right: other}
}
func (e textExpr) Below(other Expr) Expr { return relationExpr{name: "below", left: e, right: other} }
func (e textExpr) Above(other Expr) Expr { return relationExpr{name: "above", left: e, right: other} }
func (e textExpr) Near(other Expr) Expr  { return relationExpr{name: "near", left: e, right: other} }
func (e textExpr) Exists() Predicate     { return existsPredicate{expr: e} }
func (e textExpr) String() string        { return fmt.Sprintf("Text(%q)", e.needle) }

func (e regexExpr) eval(frame Frame) []matchBox {
	if strings.TrimSpace(e.pattern) == "" || e.re == nil {
		return nil
	}
	var out []matchBox
	for y, line := range frame.Lines {
		loc := e.re.FindStringIndex(line)
		if loc == nil {
			continue
		}
		out = append(out, matchBox{X: loc[0], Y: y, W: loc[1] - loc[0]})
	}
	return out
}
func (e regexExpr) LeftOf(other Expr) Expr {
	return relationExpr{name: "left_of", left: e, right: other}
}
func (e regexExpr) RightOf(other Expr) Expr {
	return relationExpr{name: "right_of", left: e, right: other}
}
func (e regexExpr) Below(other Expr) Expr { return relationExpr{name: "below", left: e, right: other} }
func (e regexExpr) Above(other Expr) Expr { return relationExpr{name: "above", left: e, right: other} }
func (e regexExpr) Near(other Expr) Expr  { return relationExpr{name: "near", left: e, right: other} }
func (e regexExpr) Exists() Predicate     { return existsPredicate{expr: e} }
func (e regexExpr) String() string        { return fmt.Sprintf("TextRE(%q)", e.pattern) }

type relationExpr struct {
	name        string
	left, right Expr
}

func (e relationExpr) eval(frame Frame) []matchBox {
	left := e.left.eval(frame)
	right := e.right.eval(frame)
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	out := make([]matchBox, 0, len(left))
	for _, l := range left {
		if relationSatisfied(e.name, l, right) {
			out = append(out, l)
		}
	}
	return out
}
func relationSatisfied(name string, left matchBox, rights []matchBox) bool {
	for _, r := range rights {
		switch name {
		case "left_of":
			if left.Y == r.Y && left.X+left.W <= r.X {
				return true
			}
		case "below":
			if left.Y > r.Y {
				return true
			}
		case "above":
			if left.Y < r.Y {
				return true
			}
		case "right_of":
			if left.Y == r.Y && left.X >= r.X+r.W {
				return true
			}
		case "near":
			dx := left.X - r.X
			if dx < 0 {
				dx = -dx
			}
			dy := left.Y - r.Y
			if dy < 0 {
				dy = -dy
			}
			if dy <= 1 && dx <= 24 {
				return true
			}
		}
	}
	return false
}
func (e relationExpr) LeftOf(other Expr) Expr {
	return relationExpr{name: "left_of", left: e, right: other}
}
func (e relationExpr) RightOf(other Expr) Expr {
	return relationExpr{name: "right_of", left: e, right: other}
}
func (e relationExpr) Below(other Expr) Expr {
	return relationExpr{name: "below", left: e, right: other}
}
func (e relationExpr) Above(other Expr) Expr {
	return relationExpr{name: "above", left: e, right: other}
}
func (e relationExpr) Near(other Expr) Expr { return relationExpr{name: "near", left: e, right: other} }
func (e relationExpr) Exists() Predicate    { return existsPredicate{expr: e} }
func (e relationExpr) String() string {
	method := map[string]string{"left_of": "LeftOf", "right_of": "RightOf", "below": "Below", "above": "Above", "near": "Near"}[e.name]
	if method == "" {
		method = e.name
	}
	return fmt.Sprintf("%s.%s(%s)", e.left.String(), method, e.right.String())
}

type existsPredicate struct{ expr Expr }

func (p existsPredicate) eval(frame Frame) bool { return len(p.expr.eval(frame)) > 0 }
func (p existsPredicate) String() string        { return fmt.Sprintf("%s.Exists()", p.expr.String()) }

type allPredicate struct{ children []Predicate }
type notPredicate struct{ child Predicate }
type withinPredicate struct {
	scope Scope
	inner Predicate
}

func All(predicates ...Predicate) Predicate { return allPredicate{children: predicates} }
func Not(predicate Predicate) Predicate     { return notPredicate{child: predicate} }
func Within(scope Scope, predicate Predicate) Predicate {
	return withinPredicate{scope: scope, inner: predicate}
}

type Scope struct{ anchor Expr }

func Box(anchor Expr) Scope { return Scope{anchor: anchor} }

func (p allPredicate) eval(frame Frame) bool {
	for _, c := range p.children {
		if !c.eval(frame) {
			return false
		}
	}
	return true
}
func (p allPredicate) String() string {
	parts := make([]string, 0, len(p.children))
	for _, c := range p.children {
		parts = append(parts, c.String())
	}
	return fmt.Sprintf("All(%s)", strings.Join(parts, ", "))
}
func (p notPredicate) eval(frame Frame) bool { return !p.child.eval(frame) }
func (p notPredicate) String() string        { return fmt.Sprintf("Not(%s)", p.child.String()) }
func (p withinPredicate) eval(frame Frame) bool {
	if p.inner == nil || p.scope.anchor == nil {
		return false
	}
	top, bottom, ok := resolveScopeRows(frame, p.scope.anchor)
	if !ok || top < 0 || bottom < top || top >= len(frame.Lines) {
		return false
	}
	if bottom >= len(frame.Lines) {
		bottom = len(frame.Lines) - 1
	}
	cropped := Frame{Lines: append([]string(nil), frame.Lines[top:bottom+1]...)}
	return p.inner.eval(cropped)
}
func (p withinPredicate) String() string {
	anchor := "<nil>"
	if p.scope.anchor != nil {
		anchor = p.scope.anchor.String()
	}
	return fmt.Sprintf("Within(Box(%s), %s)", anchor, p.inner.String())
}

func resolveScopeRows(frame Frame, anchor Expr) (int, int, bool) {
	if rel, ok := anchor.(relationExpr); ok && (rel.name == "above" || rel.name == "below") {
		left := rel.left.eval(frame)
		right := rel.right.eval(frame)
		if len(left) == 0 || len(right) == 0 {
			return 0, 0, false
		}
		top := len(frame.Lines)
		bottom := -1
		for _, l := range left {
			for _, r := range right {
				if rel.name == "above" && l.Y < r.Y {
					if l.Y < top {
						top = l.Y
					}
					if r.Y > bottom {
						bottom = r.Y
					}
				}
				if rel.name == "below" && l.Y > r.Y {
					if r.Y < top {
						top = r.Y
					}
					if l.Y > bottom {
						bottom = l.Y
					}
				}
			}
		}
		if bottom < top {
			return 0, 0, false
		}
		return top, bottom, true
	}
	boxes := anchor.eval(frame)
	if len(boxes) == 0 {
		return 0, 0, false
	}
	top, bottom := boxes[0].Y, boxes[0].Y
	for _, b := range boxes[1:] {
		if b.Y < top {
			top = b.Y
		}
		if b.Y > bottom {
			bottom = b.Y
		}
	}
	return top, bottom, true
}

func EvalNow(screen string, predicate Predicate) error {
	return EvalNowView(buf.FromString(screen), predicate)
}

func EvalNowView(view buf.View, predicate Predicate) error {
	if predicate.eval(ParseView(view)) {
		return nil
	}
	return fmt.Errorf("predicate failed now: %s", predicate.String())
}

func EvalEventually(timeout time.Duration, snapshot func() string, predicate Predicate) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is required")
	}
	return EvalEventuallyView(timeout, func() buf.View { return buf.FromString(snapshot()) }, predicate)
}

func EvalEventuallyView(timeout time.Duration, snapshot func() buf.View, predicate Predicate) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is required")
	}
	err := tempo.Eventually(timeout, 50*time.Millisecond, func() bool {
		return predicate.eval(ParseView(snapshot()))
	})
	if err != nil {
		return fmt.Errorf("predicate failed within %s: %s", timeout, predicate.String())
	}
	return nil
}
