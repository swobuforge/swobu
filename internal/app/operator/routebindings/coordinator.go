// Package routebindings serializes Share binding commits against destructive
// workspace and route identity mutations.
package routebindings

import "sync"

type Coordinator struct{ mu sync.Mutex }

func (c *Coordinator) Lock() func() { c.mu.Lock(); return c.mu.Unlock }
