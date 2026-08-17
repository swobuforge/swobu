// Package cacheintent owns portable cache-locality execution intent. It does
// not model canonical request semantics, cache materialization, or persistence.
package cacheintent

import (
	"crypto/sha256"
	"encoding/hex"
)

const derivedDomain = "cache-affinity:v1"

// Affinity is an opaque identity used to colocate related provider attempts.
// Equality is exact; callers must not normalize client-owned keys.
type Affinity struct{ key string }

// Explicit preserves one client-supplied affinity key exactly.
func Explicit(key string) Affinity { return Affinity{key: key} }

// Derived creates a compact, non-revealing identity for one workspace lineage.
func Derived(workspace, lineage string) Affinity {
	sum := sha256.Sum256([]byte(derivedDomain + "\x00" + workspace + "\x00" + lineage))
	return Affinity{key: "swobu_" + hex.EncodeToString(sum[:])}
}

func (a Affinity) Key() string  { return a.key }
func (a Affinity) IsZero() bool { return a.key == "" }
