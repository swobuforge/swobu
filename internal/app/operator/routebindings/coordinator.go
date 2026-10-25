// Package routebindings serializes Access grant/credential changes with destructive
// Workspace and route identity mutations.
package routebindings

import "sync"

type Coordinator struct{ mu sync.Mutex }

func (c *Coordinator) Lock() func() { c.mu.Lock(); return c.mu.Unlock }
