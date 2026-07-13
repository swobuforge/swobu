// swobu:lint ignore test-only-dead-cluster because=LowerAssert is a test assertion helper for the retained core lowering seam.
package corelower

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

// EnvConfig carries lowering-time knobs. It is empty for the first bridge pass.
type EnvConfig struct{}

// Lower turns one semantic core tree into one retained rendergraph tree.
//
// Lower does NOT validate the node tree at runtime. Validation is a
// compile-time / test-time assertion per the terminalui architecture.
// Use LowerAssert in tests to enforce contracts.
func Lower(root core.Node, env EnvConfig) (layout.RenderNode, error) {
	switch root.Kind() {
	case core.KindText:
		return lowerText(root, env)
	case core.KindBox:
		return lowerBox(root, env)
	case core.KindStack:
		return lowerStack(root, env)
	case core.KindLayer:
		return lowerLayer(root, env)
	case core.KindScroll:
		return lowerScroll(root, env)
	case core.KindInput:
		return lowerInput(root, env)
	case core.KindAction:
		return lowerAction(root, env)
	default:
		return nil, fmt.Errorf("unsupported core node kind %v", root.Kind())
	}
}

// LowerAssert is Lower with an assertion that the node tree is valid.
// Use this in tests; never use it in production codepaths.
func LowerAssert(root core.Node, env EnvConfig) (layout.RenderNode, error) {
	if diags := core.Validate(root); len(diags) > 0 {
		return nil, fmt.Errorf("invalid core node: %s", joinDiagnostics(diags))
	}
	return Lower(root, env)
}

func lowerNode(n core.Node, env EnvConfig) (layout.RenderNode, error) {
	switch n.Kind() {
	case core.KindText:
		return lowerText(n, env)
	case core.KindBox:
		return lowerBox(n, env)
	case core.KindStack:
		return lowerStack(n, env)
	case core.KindLayer:
		return lowerLayer(n, env)
	case core.KindScroll:
		return lowerScroll(n, env)
	case core.KindInput:
		return lowerInput(n, env)
	case core.KindAction:
		return lowerAction(n, env)
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
