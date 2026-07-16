package trafficevidence

import "strings"

type EventKind string

const (
	EventKindProviderInflight EventKind = "provider_inflight"
	EventKindProviderTerminal EventKind = "provider_terminal"
)

type ClientProtocol string

const ClientProtocolUnknown ClientProtocol = "unknown"

// ClientHandler is the primary client identifier extracted from the raw
// User-Agent header (typically the first Product/Version token).
type ClientHandler string

const ClientHandlerUnknown ClientHandler = "unknown"

// NormalizeClientHandler extracts the primary client identifier from the raw
// User-Agent header value (the first token, typically "Product/Version"),
// trimming leading and trailing whitespace. Falls back to ClientHandlerUnknown
// when blank.
func NormalizeClientHandler(raw string) ClientHandler {
	label := strings.TrimSpace(raw)
	if label == "" {
		return ClientHandlerUnknown
	}
	// Keep only the first token; multi-token UAs (e.g.,
	// "opencode/1.15.13 ai-sdk/... runtime/bun/...") should collapse to the
	// primary product so the Cockpit activity row stays compact.
	if idx := strings.IndexFunc(label, func(r rune) bool { return r == ' ' || r == '\t' }); idx >= 0 {
		label = label[:idx]
	}
	return ClientHandler(label)
}

type ClientFamily string

const ClientFamilyUnknown ClientFamily = "unknown"

type NormalizedOp string

const NormalizedOpUnknown NormalizedOp = "unknown"
