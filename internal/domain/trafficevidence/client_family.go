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

const (
	ClientFamilyCodex      ClientFamily = "codex"
	ClientFamilyClaudeCode ClientFamily = "claude_code"
	ClientFamilyCline      ClientFamily = "cline"
	ClientFamilyOpenCode   ClientFamily = "opencode"
	ClientFamilyAider      ClientFamily = "aider"
	ClientFamilyOther      ClientFamily = "other"
	ClientFamilyUnknown    ClientFamily = "unknown"
)

// ClassifyClientFamily projects a raw HTTP User-Agent into the closed product
// family vocabulary admitted by traffic evidence. The raw header remains an
// ingress-only fact and never enters ProductReport.
func ClassifyClientFamily(raw string) ClientFamily {
	product := strings.ToLower(string(NormalizeClientHandler(raw)))
	if slash := strings.IndexByte(product, '/'); slash >= 0 {
		product = product[:slash]
	}
	switch product {
	case "codex":
		return ClientFamilyCodex
	case "claude-code", "claude_code":
		return ClientFamilyClaudeCode
	case "cline":
		return ClientFamilyCline
	case "opencode":
		return ClientFamilyOpenCode
	case "aider":
		return ClientFamilyAider
	case "", string(ClientHandlerUnknown):
		return ClientFamilyUnknown
	default:
		return ClientFamilyOther
	}
}

type NormalizedOp string

const NormalizedOpUnknown NormalizedOp = "unknown"
