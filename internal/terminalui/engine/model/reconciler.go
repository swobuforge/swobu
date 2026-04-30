package model

import "fmt"

type Reconciler struct{}

func (Reconciler) Reconcile(prev Node, next Node, mode Mode) []OpRecord {
	prevN := normalize(prev)
	nextN := normalize(next)
	switch mode {
	case ModeLive:
		return reconcileLive(prevN, nextN)
	case ModeFullscreen:
		return reconcileFullscreen(prevN, nextN)
	default:
		return reconcileAppend(prevN, nextN)
	}
}

func reconcileAppend(prev Node, next Node) []OpRecord {
	prevDurable := collect(prev, Durable)
	nextDurable := collect(next, Durable)
	start := longestCommonPrefix(prevDurable, nextDurable)
	ops := make([]OpRecord, 0, len(nextDurable)-start)
	for i := start; i < len(nextDurable); i++ {
		ops = append(ops, OpRecord{Kind: OpAppendLine, Line: nextDurable[i]})
	}
	return ops
}

func reconcileLive(prev Node, next Node) []OpRecord {
	ops := reconcileAppend(prev, next)
	p := last(collect(prev, Ephemeral))
	n := last(collect(next, Ephemeral))
	if n != "" && n != p {
		ops = append(ops, OpRecord{Kind: OpUpdateStatus, Line: n})
	}
	return ops
}

func reconcileFullscreen(prev Node, next Node) []OpRecord {
	p := collect(prev, Durable)
	pn := collect(prev, Ephemeral)
	if len(pn) > 0 {
		p = append(p, pn...)
	}
	n := collect(next, Durable)
	nn := collect(next, Ephemeral)
	if len(nn) > 0 {
		n = append(n, nn...)
	}
	if equalSlice(p, n) {
		return nil
	}
	return []OpRecord{{Kind: OpPaintFrame, Lines: n}}
}

func normalize(root Node) Node {
	return norm(root, "root")
}

func norm(n Node, path string) Node {
	if n.Key == "" {
		n.Key = path
	}
	for i := range n.Children {
		childPath := fmt.Sprintf("%s/%s[%d]", n.Key, n.Children[i].Kind, i)
		n.Children[i] = norm(n.Children[i], childPath)
	}
	return n
}

func collect(n Node, want Durability) []string {
	out := make([]string, 0)
	walkCollect(n, want, &out)
	return out
}

func walkCollect(n Node, want Durability, out *[]string) {
	if n.Durability == want && n.Text != "" {
		*out = append(*out, n.Text)
	}
	for _, c := range n.Children {
		walkCollect(c, want, out)
	}
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
