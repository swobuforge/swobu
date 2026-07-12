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
func Lower(root core.Node, env EnvConfig) (layout.RenderNode, error) {
	if diagnostics := core.Validate(root); len(diagnostics) > 0 {
		return nil, fmt.Errorf("invalid core node: %s", joinDiagnostics(diagnostics))
	}
	return lowerNode(root, env)
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
