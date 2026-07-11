package ports

import "github.com/swobuforge/swobu/internal/domain/canonical"

// ContinuationStore is the semantic continuation port exposed to bootstrap and
// application wiring. It aliases the canonical continuation store so the
// semantic chain contract has one definition.
type ContinuationStore = canonical.ContinuationStore
