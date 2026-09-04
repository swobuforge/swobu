package openaifamily

import (
	"net/http"
	"strings"
)

// AuthHeaderName identifies the wire header that carries a provider token.
type AuthHeaderName string

const (
	AuthHeaderAuthorization AuthHeaderName = "Authorization"
	AuthHeaderXAPIKey       AuthHeaderName = "X-API-Key"
	AuthHeaderAPIKey        AuthHeaderName = "api-key"
)

// AuthStyle describes how a token is rendered into the request header.
type AuthStyle string

const (
	AuthStyleNone   AuthStyle = "none"
	AuthStyleBearer AuthStyle = "bearer"
	AuthStyleValue  AuthStyle = "value"
)

// AuthStrategy owns the provider-specific token header contract.
type AuthStrategy struct {
	Header AuthHeaderName
	Style  AuthStyle
}

// NoAuthStrategy returns a provider policy that sends no auth header.
func NoAuthStrategy() AuthStrategy {
	return AuthStrategy{Style: AuthStyleNone}
}

// BearerAuthStrategy returns a bearer-token auth policy.
func BearerAuthStrategy() AuthStrategy {
	return AuthStrategy{Header: AuthHeaderAuthorization, Style: AuthStyleBearer}
}

// XAPIKeyAuthStrategy returns an X-API-Key auth policy.
func XAPIKeyAuthStrategy() AuthStrategy {
	return AuthStrategy{Header: AuthHeaderXAPIKey, Style: AuthStyleValue}
}

// APIKeyAuthStrategy returns an api-key auth policy.
func APIKeyAuthStrategy() AuthStrategy {
	return AuthStrategy{Header: AuthHeaderAPIKey, Style: AuthStyleValue}
}

// AuthStrategyForHeader resolves a selected or default header contract.
func AuthStrategyForHeader(header string, fallback AuthStrategy) AuthStrategy {
	header = strings.TrimSpace(header) // swobu:io-string source=boundary
	switch {
	case header == "":
		return fallback
	case strings.EqualFold(header, string(AuthHeaderAuthorization)):
		return BearerAuthStrategy()
	case strings.EqualFold(header, string(AuthHeaderXAPIKey)):
		return XAPIKeyAuthStrategy()
	case strings.EqualFold(header, string(AuthHeaderAPIKey)):
		return APIKeyAuthStrategy()
	default:
		return AuthStrategy{Header: AuthHeaderName(header), Style: AuthStyleValue}
	}
}

// Apply writes the transport-owned token into the provider-selected header.
func (s AuthStrategy) Apply(req *http.Request, token string) {
	if req == nil || s.Style == AuthStyleNone {
		return
	}
	switch s.Style {
	case AuthStyleBearer:
		req.Header.Set(string(s.Header), "Bearer "+token)
	default:
		req.Header.Set(string(s.Header), token)
	}
}
