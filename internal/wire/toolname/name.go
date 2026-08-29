// Package toolname owns the flat provider tool-name spelling grammar.
package toolname

import (
	"crypto/sha256"
	"encoding/base32"
	"strings"
)

// MaxLength stays below the common 64-character declaration ceiling because
// Gemini can normalize a generated name at that exact boundary when it emits
// a function call, breaking exact reverse provenance.
const MaxLength = 63

const GeneratedPrefix = "s__"

const generatedHashLength = 16

var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func Safe(name string) bool {
	if name == "" || len(name) > MaxLength {
		return false
	}
	for i := 0; i < len(name); i++ {
		value := name[i]
		if !(value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_' || value == '-') {
			return false
		}
	}
	return true
}

func PreservableLiteral(name string) bool {
	return Safe(name) && !strings.HasPrefix(name, GeneratedPrefix)
}

// Generated returns one stable bounded provider spelling for a semantic tool
// identity. The returned digest is a collision guard; reverse routing remains
// the responsibility of the attempt-scoped dictionary.
func Generated(identity string, scope []string, leaf string) string {
	parts := make([]string, 0, len(scope)+1)
	for _, part := range append(append([]string(nil), scope...), leaf) {
		if normalized := normalize(part); normalized != "" {
			parts = append(parts, normalized)
		}
	}
	readable := strings.Join(parts, "__")
	if readable == "" {
		readable = "tool"
	}
	digest := sha256.Sum256([]byte("swobu-tool-wire-v1\x00" + identity))
	hash := strings.ToLower(encoding.EncodeToString(digest[:]))[:generatedHashLength] // swobu:io-string source=domain
	limit := MaxLength - len(GeneratedPrefix) - 2 - len(hash)
	if len(readable) > limit {
		readable = readable[:limit]
	}
	return GeneratedPrefix + readable + "__" + hash
}

func normalize(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		char := value[i]
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			out.WriteByte(char)
		} else {
			out.WriteByte('_')
		}
	}
	if out.Len() == 0 {
		return "tool"
	}
	return out.String()
}
