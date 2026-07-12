package openaifamily

import (
	"net/http"
	"strings"
)

type authHeaderName string

const (
	authHeaderAuthorization authHeaderName = "Authorization"
	authHeaderXAPIKey       authHeaderName = "X-API-Key"
	authHeaderAPIKey        authHeaderName = "api-key"
)

type authHeaderStyle string

const (
	authStyleNone   authHeaderStyle = "none"
	authStyleBearer authHeaderStyle = "bearer"
	authStyleValue  authHeaderStyle = "value"
)

type authStrategy struct {
	Header authHeaderName
	Style  authHeaderStyle
}

func noAuthStrategy() authStrategy {
	return authStrategy{Style: authStyleNone}
}

func bearerAuthStrategy() authStrategy {
	return authStrategy{Header: authHeaderAuthorization, Style: authStyleBearer}
}

func xAPIKeyAuthStrategy() authStrategy {
	return authStrategy{Header: authHeaderXAPIKey, Style: authStyleValue}
}

func apiKeyAuthStrategy() authStrategy {
	return authStrategy{Header: authHeaderAPIKey, Style: authStyleValue}
}

func authStrategyForHeader(header string, fallback authStrategy) authStrategy {
	header = strings.TrimSpace(header) // swobu:io-string source=boundary
	switch {
	case header == "":
		return fallback
	case strings.EqualFold(header, string(authHeaderAuthorization)):
		return bearerAuthStrategy()
	case strings.EqualFold(header, string(authHeaderXAPIKey)):
		return xAPIKeyAuthStrategy()
	case strings.EqualFold(header, string(authHeaderAPIKey)):
		return apiKeyAuthStrategy()
	default:
		return authStrategy{Header: authHeaderName(header), Style: authStyleValue}
	}
}

func (s authStrategy) apply(req *http.Request, token string) {
	if req == nil || s.Style == authStyleNone {
		return
	}
	if got := req.Header.Get(string(s.Header)); got != "" {
		return
	}
	switch s.Style {
	case authStyleBearer:
		req.Header.Set(string(s.Header), "Bearer "+token)
	default:
		req.Header.Set(string(s.Header), token)
	}
}
