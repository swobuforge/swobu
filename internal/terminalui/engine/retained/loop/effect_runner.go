package loop

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
)

// FollowUp returns the channel through which async effects report results.
// The host event loop must select on this alongside input events.
func (loop *AppLoop[M]) FollowUp() <-chan []update.Action {
	return loop.followUp
}

// Accept sends actions back into the loop from an async effect.
func (loop *AppLoop[M]) Accept(actions []update.Action) {
	ctx := loop.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case loop.followUp <- actions:
	case <-ctx.Done():
	}
}

// SetContext stores the cancellation context for effect execution.
func (loop *AppLoop[M]) SetContext(ctx context.Context) {
	loop.ctx = ctx
}

// Dispatch applies one or more actions through the reducer.
// Effects returned by the reducer are executed asynchronously.
func (loop *AppLoop[M]) Dispatch(actions []update.Action) {
	if len(actions) > 0 {
		loop.Invalidate()
	}
	for _, a := range actions {
		if loop.handleRuntimeAction(a) {
			continue
		}
		effects := loop.Reduce(&loop.Model, a)
		for _, eff := range effects {
			loop.executeEffect(eff)
		}
	}
}

func (loop *AppLoop[M]) handleRuntimeAction(action update.Action) bool {
	switch cmd := action.(type) {
	case interaction.FocusMoveAction:
		switch cmd.Move {
		case interaction.FocusMoveNext:
			loop.FocusNext()
		case interaction.FocusMovePrev:
			loop.FocusPrev()
		}
		return true
	case interaction.FocusKeyAction:
		key := strings.TrimSpace(cmd.Key)
		if key == "" {
			return true
		}
		// Resolve after rebuild; named identity can point to post-action tree.
		loop.pendingFocusKey = key
		return true
	default:
		return false
	}
}

func firstFocusableByKey(root *layout.LayoutNode, key string) *layout.LayoutNode {
	var found *layout.LayoutNode
	var walk func(*layout.LayoutNode)
	walk = func(node *layout.LayoutNode) {
		if node == nil || found != nil {
			return
		}
		_, nodeKey, _ := view.NamedNodeMetadata(layout.UnwrapIdentity(node.RenderNode))
		if nodeKey == key {
			if canFocus(node) {
				found = node
				return
			}
			if nested := firstFocusableInSubtree(node); nested != nil {
				found = nested
				return
			}
		}
		for _, child := range stableKids(node.Kids) {
			walk(child)
		}
	}
	walk(root)
	return found
}

func firstFocusableInSubtree(node *layout.LayoutNode) *layout.LayoutNode {
	if node == nil {
		return nil
	}
	for _, child := range stableKids(node.Kids) {
		if canFocus(child) {
			return child
		}
		if nested := firstFocusableInSubtree(child); nested != nil {
			return nested
		}
	}
	return nil
}

func (loop *AppLoop[M]) executeEffect(eff update.Effect) {
	ctx := loop.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		actions := eff.Execute(ctx)
		select {
		case loop.followUp <- actions:
		case <-ctx.Done():
		}
	}()
}
