package retained

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/terminalui/component"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/corelower"
)

// CoreViewSpec is the semantic component contract accepted by the retained
// adapter layer.
type CoreViewSpec[M any] interface {
	BuildCoreNode(ctx *component.Context[M]) core.Node
}

type componentLocalScopeAdapter struct {
	scope LocalScope
}

func (s componentLocalScopeAdapter) Get(slot int) (any, bool) {
	if s.scope == nil {
		return nil, false
	}
	return s.scope.Get(slot)
}

func (s componentLocalScopeAdapter) Set(slot int, value any) {
	if s.scope == nil {
		return
	}
	s.scope.Set(slot, value)
}

func (s componentLocalScopeAdapter) WithPrefix(prefix string) component.LocalScope {
	if s.scope == nil {
		return componentLocalScopeAdapter{}
	}
	return componentLocalScopeAdapter{scope: s.scope.WithPrefix(prefix)}
}

// FromCore lowers one semantic component view into the retained view contract.
func FromCore[M any](v component.View[M]) ViewSpec[M] {
	if v == nil {
		return nil
	}
	return View[M](func(ctx *Context[M]) RenderNode {
		if ctx == nil {
			return nil
		}
		cctx := component.NewContext(component.ComponentRuntimeState[M]{
			Local:    componentLocalScopeAdapter{scope: ctx.Local},
			Model:    ctx.Model,
			Dispatch: ctx.dispatch,
			Emit:     ctx.emit,
			Building: ctx.building,
		})
		node := v.BuildCoreNode(cctx)
		renderNode, err := corelower.Lower(node, corelower.EnvConfig{})
		if err != nil {
			panic(fmt.Sprintf("corelower failed: %v", err))
		}
		return renderNode
	})
}
