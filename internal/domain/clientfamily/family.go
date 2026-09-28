// Package clientfamily owns the bounded client product identity shared by
// execution evidence and privacy-safe product telemetry.
package clientfamily

import "strings"

type Family string

const (
	Codex      Family = "codex"
	ClaudeCode Family = "claude_code"
	Cline      Family = "cline"
	OpenCode   Family = "opencode"
	Aider      Family = "aider"
	VSCode     Family = "vscode"
	Other      Family = "other"
	Unknown    Family = "unknown"
)

func (f Family) Valid() bool {
	switch f {
	case Codex, ClaudeCode, Cline, OpenCode, Aider, VSCode, Other, Unknown:
		return true
	}
	return false
}

// Classify discards version-bearing User-Agent details and bounds the first
// product token. Neither the raw header nor its version is execution identity.
func Classify(raw string) Family {
	product := strings.ToLower(rawFirstToken(raw))
	if index := strings.IndexByte(product, '/'); index >= 0 {
		product = product[:index]
	}
	switch product {
	case "codex":
		return Codex
	case "claude-code", "claude_code":
		return ClaudeCode
	case "cline":
		return Cline
	case "opencode":
		return OpenCode
	case "aider":
		return Aider
	case "swobu-vscode":
		return VSCode
	case "", "unknown":
		return Unknown
	default:
		return Other
	}
}

func rawFirstToken(raw string) string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
