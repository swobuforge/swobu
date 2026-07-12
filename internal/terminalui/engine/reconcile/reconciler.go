package reconcile

import "github.com/swobuforge/swobu/internal/terminalui/transcript"

type Reconciler struct{}

func (Reconciler) Reconcile(prev transcript.ViewSpec, next transcript.ViewSpec, mode transcript.RenderMode) []RenderOpEntry {
	prevN := transcript.Normalize(prev)
	nextN := transcript.Normalize(next)
	return Reconciler{}.ReconcileScene(transcript.Project(prevN), transcript.Project(nextN), mode)
}

func (Reconciler) ReconcileScene(prev transcript.SceneSnapshot, next transcript.SceneSnapshot, mode transcript.RenderMode) []RenderOpEntry {
	switch mode {
	case transcript.RenderModeLive:
		return reconcileLive(prev, next)
	case transcript.RenderModeFullscreen:
		return reconcileFullscreen(prev, next)
	default:
		return reconcileAppend(prev, next)
	}
}

func reconcileAppend(prev transcript.SceneSnapshot, next transcript.SceneSnapshot) []RenderOpEntry {
	prevDurable := prev.Durable
	nextDurable := next.Durable
	start := longestCommonPrefix(prevDurable, nextDurable)
	ops := make([]RenderOpEntry, 0, len(nextDurable)-start)
	for i := start; i < len(nextDurable); i++ {
		ops = append(ops, RenderOpEntry{Kind: RenderOpAppendDurableLine, Text: nextDurable[i]})
	}
	return ops
}

func reconcileLive(prev transcript.SceneSnapshot, next transcript.SceneSnapshot) []RenderOpEntry {
	ops := reconcileAppend(prev, next)
	p := last(prev.Ephemeral)
	n := last(next.Ephemeral)
	if n != "" && n != p {
		ops = append(ops, RenderOpEntry{Kind: RenderOpUpdateEphemeralLine, Text: n})
	}
	return ops
}

func reconcileFullscreen(prev transcript.SceneSnapshot, next transcript.SceneSnapshot) []RenderOpEntry {
	p := prev.Durable
	pn := prev.Ephemeral
	if len(pn) > 0 {
		p = append(p, pn...)
	}
	n := next.Durable
	nn := next.Ephemeral
	if len(nn) > 0 {
		n = append(n, nn...)
	}
	if equalSlice(p, n) {
		return nil
	}
	return []RenderOpEntry{{Kind: RenderOpPaintFrame, FrameLines: n}}
}

func longestCommonPrefix(a, b []string) int {
	l := len(a)
	if len(b) < l {
		l = len(b)
	}
	i := 0
	for i < l && a[i] == b[i] {
		i++
	}
	return i
}

func last(in []string) string {
	if len(in) == 0 {
		return ""
	}
	return in[len(in)-1]
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
