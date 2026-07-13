// Package corelower bridges semantic core nodes into retained rendergraph nodes.
//
// It is a hard compiler: invalid core trees always produce errors, never
// render nodes. This boundary validates semantic integrity before the retained
// engine sees any layout or paint work.
//
// Intent routing and style resolution live here.
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
	// If nil, DefaultColorConfig is used.
	Resolver *StyleResolver
}

// Lower turns one semantic core tree into one retained rendergraph tree.
//
// Lower is a hard compiler: it always validates the core tree before lowering.
// If validation produces any diagnostics, Lower returns an error and no render node.
// Invalid UI trees must never reach the retained engine.
func Lower[E any](root core.Node[E], env EnvConfig, caster EventCaster[E]) (layout.RenderNode, error) {
	if diags := core.Validate(root); len(diags) > 0 {
		return nil, fmt.Errorf("invalid core node: %s", joinDiagnostics(diags))
	}
	return lowerNode(root, env, caster)
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
	case core.KindList, core.KindTable:
		return nil, fmt.Errorf("unsupported core node kind %v (not yet implemented)", n.Kind())
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
