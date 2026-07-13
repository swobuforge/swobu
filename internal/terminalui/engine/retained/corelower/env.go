// swobu:lint ignore test-only-dead-cluster because=LowerAssert is a test assertion helper for the retained core lowering seam.
package corelower

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// EventCaster converts a typed event to a retained update.Action.
type EventCaster[E any] func(E) update.Action

// EnvConfig carries lowering-time knobs. It is empty for the first bridge pass.
type EnvConfig struct {
	// Resolver maps semantic style tokens to concrete paint styles.
	// If nil, DefaultPalette is used.
	Resolver *StyleResolver

	// DevMode enables runtime validation and inline diagnostic rendering.
	// When true, Lower validates the node tree before lowering and renders
	// any validation failures as visible error nodes instead of returning
	// nil.  Production codepaths should leave this false.
	DevMode bool
}

// Lower turns one semantic core tree into one retained rendergraph tree.
//
// When env.DevMode is true, Lower validates the tree first.  If validation
// finds errors it renders them as inline diagnostic nodes (red text boxes)
// instead of returning nil.  Production codepaths should leave DevMode false.
//
// LowerAssert in tests enforces validation by always treating the tree as
// invalid when diagnostics exist and returning an error; see LowerAssert.
func Lower[E any](root core.Node[E], env EnvConfig, caster EventCaster[E]) (layout.RenderNode, error) {
	if env.DevMode {
		if diags := core.Validate(root); len(diags) > 0 {
			diagEnv := env
			diagEnv.DevMode = false // Do not re-validate the diagnostic tree.
			return Lower(core.NewDiagnosticNode[E](diags), diagEnv, caster)
		}
	}
	switch root.Kind() {
	case core.KindText:
		return lowerText(root, env)
	case core.KindBox:
		return lowerBox(root, env, caster)
	case core.KindStack:
		return lowerStack(root, env, caster)
	case core.KindLayer:
		return lowerLayer(root, env, caster)
	case core.KindScroll:
		return lowerScroll(root, env, caster)
	case core.KindInput:
		return lowerInput(root, env, caster)
	case core.KindAction:
		return lowerAction(root, env, caster)
	default:
		return nil, fmt.Errorf("unsupported core node kind %v", root.Kind())
	}
}

// LowerAssert is Lower with an assertion that the node tree is valid.
// Use this in tests; never use it in production codepaths.
func LowerAssert[E any](root core.Node[E], env EnvConfig, caster EventCaster[E]) (layout.RenderNode, error) {
	if diags := core.Validate(root); len(diags) > 0 {
		return nil, fmt.Errorf("invalid core node: %s", joinDiagnostics(diags))
	}
	return Lower(root, env, caster)
}

func lowerNode[E any](n core.Node[E], env EnvConfig, caster EventCaster[E]) (layout.RenderNode, error) {
	switch n.Kind() {
	case core.KindText:
		return lowerText(n, env)
	case core.KindBox:
		return lowerBox(n, env, caster)
	case core.KindStack:
		return lowerStack(n, env, caster)
	case core.KindLayer:
		return lowerLayer(n, env, caster)
	case core.KindScroll:
		return lowerScroll(n, env, caster)
	case core.KindInput:
		return lowerInput(n, env, caster)
	case core.KindAction:
		return lowerAction(n, env, caster)
	default:
		return nil, fmt.Errorf("unsupported core node kind %v", n.Kind())
	}
}

func joinDiagnostics(diags []core.Diagnostic) string {
	msgs := make([]string, 0, len(diags))
	for _, d := range diags {
		msgs = append(msgs, fmt.Sprintf("%s: %s", d.Path, d.Message))
	}
	return strings.Join(msgs, "; ")
}
