// Package toolname owns the flat provider tool-name spelling grammar.
package toolname

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"strings"
)

const MaxLength = 64

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

// Alias returns a bounded collision-resistant spelling. Collision ownership
// remains with the attempt-local projection table.
func Alias(identity, readable string, ordinal uint32) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", identity, ordinal)))
	encoded := strings.ToLower(encoding.EncodeToString(digest[:])) // swobu:io-string source=domain
	prefix := normalize(readable)
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return "s_" + prefix + "__" + encoded
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
