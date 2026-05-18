package httpapi

import (
	"net/http"
	"strings"
	"unicode"

	"github.com/swobuforge/swobu/internal/app/requestpath"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

const (
	clientProtocolOpenAICompat    = "openai_compat"
	clientProtocolAnthropicCompat = "anthropic_compat"
	clientProtocolUnknown         = "unknown"
	clientHandlerUnknown          = "unknown"
)

func ingressProvenance(r *http.Request, family canonical.IngressFamily, normalizedPath canonical.NormalizedPath) requestpath.IngressProvenance {
	return requestpath.IngressProvenance{
		ClientProtocol: classifyClientProtocol(family),
		IngressFamily:  family,
		NormalizedOp:   normalizedPath,
		ClientHandler:  classifyClientHandler(r),
	}
}

func classifyClientProtocol(family canonical.IngressFamily) string {
	switch family {
	case canonical.IngressFamilyChatCompletions, canonical.IngressFamilyResponses, canonical.IngressFamilyCompletions:
		return clientProtocolOpenAICompat
	case canonical.IngressFamilyMessages:
		return clientProtocolAnthropicCompat
	default:
		return clientProtocolUnknown
	}
}

func classifyClientHandler(r *http.Request) string {
	if r == nil {
		return clientHandlerUnknown
	}
	ua := strings.ToLower(strings.TrimSpace(r.Header.Get("User-Agent"))) // swobu:io-string source=boundary
	if ua != "" {
		token := strings.Fields(ua)[0]
		token = strings.TrimSpace(strings.SplitN(token, "/", 2)[0]) // swobu:io-string source=boundary
		normalized := normalizeHandlerToken(token)
		if normalized != "" {
			return normalized
		}
	}
	if lang := normalizeHandlerToken(strings.TrimSpace(r.Header.Get("X-Stainless-Lang"))); lang != "" { // swobu:io-string source=boundary
		return "stainless_" + lang
	}
	return clientHandlerUnknown
}

func normalizeHandlerToken(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw)) // swobu:io-string source=boundary
	if raw == "" {
		return ""
	}
	var out strings.Builder
	out.Grow(len(raw))
	lastUnderscore := false
	for _, r := range raw {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			out.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				out.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	token := strings.Trim(out.String(), "_")
	if token == "" {
		return ""
	}
	if len(token) > 48 {
		token = token[:48]
	}
	return token
}
