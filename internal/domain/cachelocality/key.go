// Package cachelocality owns portable cache locality: an opaque key used to
// prefer a cache domain for requests expected to reuse provider prompt state.
// It does not model conversation identity, prefix equality, cache
// materialization, retention, or persistence.
package cachelocality

import (
	"crypto/sha256"
	"encoding/hex"
)

// derivedDomain is a compatibility token. Changing it would remap existing
// derived locality keys and defeat the cache reuse this package preserves.
const derivedDomain = "cache-affinity:v1"

// Key is an opaque cache-locality key. Equality is exact; callers must not
// normalize client-owned keys or assume equal keys prove equal prompt prefixes.
type Key struct{ key string }

// Explicit preserves one client-supplied cache-locality key exactly.
func Explicit(key string) Key { return Key{key: key} }

// Derived creates a compact, non-revealing locality heuristic for one workspace
// lineage. The lineage remains the conversation-continuity authority.
func Derived(workspace, lineage string) Key {
	sum := sha256.Sum256([]byte(derivedDomain + "\x00" + workspace + "\x00" + lineage))
	return Key{key: "swobu_" + hex.EncodeToString(sum[:])}
}

func (a Key) Key() string  { return a.key }
func (a Key) IsZero() bool { return a.key == "" }
