package profile

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizeRunPodEndpoint converts a Runpod endpoint ID/slug into the
// executable OpenAI-compatible base URL. Full HTTP(S) URLs are preserved
// except for trailing-slash normalization. The helper is pure and performs no
// Runpod control-plane or inference I/O.
func NormalizeRunPodEndpoint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("endpoint is required")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("endpoint is malformed")
	}
	if parsed.Scheme != "" || strings.Contains(raw, "://") {
		if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return "", fmt.Errorf("endpoint must be an absolute HTTP(S) URL or Runpod endpoint ID")
		}
		// Trim only literal slashes at the end of the URL path. Trimming the
		// raw string would corrupt a query or fragment whose value happens to
		// end in a slash, while the full URL override must otherwise survive
		// authoring unchanged.
		escapedPath := parsed.EscapedPath()
		if !strings.HasSuffix(escapedPath, "/") {
			return raw, nil
		}
		escapedPath = strings.TrimRight(escapedPath, "/")
		path, err := url.PathUnescape(escapedPath)
		if err != nil {
			return "", fmt.Errorf("endpoint is malformed")
		}
		parsed.Path = path
		parsed.RawPath = escapedPath
		if parsed.RawPath == parsed.Path {
			parsed.RawPath = ""
		}
		return parsed.String(), nil
	}
	if strings.ContainsAny(raw, "\r\n") {
		return "", fmt.Errorf("endpoint is malformed")
	}
	return "https://api.runpod.ai/v2/" + url.PathEscape(raw) + "/openai/v1", nil
}
