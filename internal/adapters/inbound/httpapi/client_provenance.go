package httpapi

import (
	"net/http"
	"strings"
	"unicode"

	"github.com/swobuforge/swobu/internal/app/requestpath"
	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

const (
	clientProtocolOpenAICompat    = "openai_compat"
	clientProtocolAnthropicCompat = "anthropic_compat"
	clientProtocolUnknown         = "unknown"
	clientHandlerUnknown          = "unknown"
)

func ingressProvenance(r *http.Request, family compatibility.IngressFamily, normalizedPath compatibility.NormalizedPath) requestpath.IngressProvenance {
	return requestpath.IngressProvenance{
		ClientProtocol: classifyClientProtocol(family),
		IngressFamily:  family,
		NormalizedOp:   normalizedPath,
		ClientHandler:  classifyClientHandler(r),
	}
}

func classifyClientProtocol(family compatibility.IngressFamily) string {
	switch family {
	case compatibility.IngressFamilyChatCompletions, compatibility.IngressFamilyResponses, compatibility.IngressFamilyCompletions:
		return clientProtocolOpenAICompat
	case compatibility.IngressFamilyMessages:
		return clientProtocolAnthropicCompat
	default:
		return clientProtocolUnknown
	}
}

func classifyClientHandler(r *http.Request) string {
	if r == nil {
		return clientHandlerUnknown
	}
	ua := strings.ToLower(strings.TrimSpace(r.Header.Get("User-Agent")))
	if ua != "" {
		token := strings.Fields(ua)[0]
		token = strings.TrimSpace(strings.SplitN(token, "/", 2)[0])
		normalized := normalizeHandlerToken(token)
		if normalized != "" {
			return normalized
		}
	}
	if lang := normalizeHandlerToken(strings.TrimSpace(r.Header.Get("X-Stainless-Lang"))); lang != "" {
		return "stainless_" + lang
	}
	return clientHandlerUnknown
}

func normalizeHandlerToken(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
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
