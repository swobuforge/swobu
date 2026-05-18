package retained

import (
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// LifecycleEffects captures mount/unmount effects for view lifecycle hooks.
type LifecycleEffects struct {
	OnMount   []update.Effect
	OnUnmount []update.Effect
}

func captureLifecycle(candidate any) LifecycleEffects {
	out := LifecycleEffects{}
	if hooks, ok := candidate.(interface{ OnMountEffects() []update.Effect }); ok {
		out.OnMount = append([]update.Effect(nil), hooks.OnMountEffects()...)
	}
	if hooks, ok := candidate.(interface{ OnUnmountEffects() []update.Effect }); ok {
		out.OnUnmount = append([]update.Effect(nil), hooks.OnUnmountEffects()...)
	}
	return out
}

// CaptureLifecycle extracts lifecycle effects from a view.
func CaptureLifecycle(candidate any) LifecycleEffects {
	if candidate == nil {
		return LifecycleEffects{}
	}
	return captureLifecycle(candidate)
}
