package core

import "strings"

// Key is the stable identity token for semantic nodes and hookable component
// instances.
type Key string

// K trims user-facing whitespace and returns one normalized key token.
func K(v string) Key {
	normalized := strings.TrimSpace(v) // swobu:io-string source=boundary
	return Key(normalized)
}

// Empty reports whether the key has no meaningful content.
func (k Key) Empty() bool {
	return string(k) == ""
}

// String returns the raw string form of the key.
func (k Key) String() string {
	return string(k)
}
