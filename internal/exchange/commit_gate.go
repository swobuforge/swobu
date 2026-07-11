package exchange

// CommitGate marks the point after which fallback can no longer silently swap
// targets because client-visible output has already been committed.
type CommitGate struct {
	committed bool
}

// NewCommitGate constructs one fresh commit gate.
func NewCommitGate() *CommitGate {
	return &CommitGate{}
}

// Commit records that client-visible output has been committed.
func (g *CommitGate) Commit() {
	if g == nil {
		return
	}
	g.committed = true
}

// Committed reports whether client-visible output has been committed.
func (g *CommitGate) Committed() bool {
	return g != nil && g.committed
}

// CanFallback reports whether a fallback target can still be selected safely.
func (g *CommitGate) CanFallback() bool {
	return g == nil || !g.committed
}
