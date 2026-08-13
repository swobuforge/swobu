package mcp

import (
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// SourceAccess is one request-private MCP credential/header projection.
// Callers must keep it attempt-local and must never persist or log its values.
type SourceAccess struct {
	Authorization string
	Headers       map[string]string
}

type accessSecrets struct {
	bySource map[canonical.ToolKey]SourceAccess
}

// Access is request-private MCP authorization and header state. It crosses
// ingress into Exchange's local MCP runtime but never enters provider
// projection or canonical history.
type Access struct {
	secrets *accessSecrets
}

// WithBearer adds the authorization-token field for one source.
func (a Access) WithBearer(source canonical.ToolKey, bearer string) (Access, error) {
	bearer = strings.TrimSpace(bearer)
	if source.IsZero() || source.Kind() != canonical.ToolKindMCP || bearer == "" {
		return Access{}, canonical.BadRequest("responses MCP authorization is invalid")
	}
	return a.withSource(source, func(current SourceAccess) (SourceAccess, error) {
		if current.Authorization != "" && current.Authorization != bearer {
			return SourceAccess{}, canonical.BadRequest("responses MCP source has contradictory bearer authorization")
		}
		for name, value := range current.Headers {
			if strings.EqualFold(name, "Authorization") &&
				!strings.EqualFold(strings.TrimSpace(value), "Bearer "+bearer) {
				return SourceAccess{}, canonical.BadRequest("responses MCP source has contradictory bearer authorization")
			}
		}
		current.Authorization = bearer
		return current, nil
	})
}

// WithHeaders adds arbitrary request headers for one source. Header names are
// compared case-insensitively so contradictory authority cannot depend on map
// order or spelling.
func (a Access) WithHeaders(source canonical.ToolKey, headers map[string]string) (Access, error) {
	if source.IsZero() || source.Kind() != canonical.ToolKindMCP {
		return Access{}, canonical.BadRequest("responses MCP headers are invalid")
	}
	return a.withSource(source, func(current SourceAccess) (SourceAccess, error) {
		if current.Headers == nil {
			current.Headers = map[string]string{}
		}
		for name, value := range headers {
			if strings.EqualFold(name, "Authorization") && current.Authorization != "" &&
				!strings.EqualFold(strings.TrimSpace(value), "Bearer "+current.Authorization) {
				return SourceAccess{}, canonical.BadRequest("responses MCP source has contradictory bearer authorization")
			}
			for priorName, priorValue := range current.Headers {
				if strings.EqualFold(priorName, name) {
					if priorValue != value {
						return SourceAccess{}, canonical.BadRequest("responses MCP source has contradictory headers")
					}
					delete(current.Headers, priorName)
					break
				}
			}
			current.Headers[name] = value
		}
		return current, nil
	})
}

func (a Access) withSource(source canonical.ToolKey, update func(SourceAccess) (SourceAccess, error)) (Access, error) {
	cloned := make(map[canonical.ToolKey]SourceAccess)
	if a.secrets != nil {
		for key, value := range a.secrets.bySource {
			cloned[key] = cloneSourceAccess(value)
		}
	}
	next, err := update(cloneSourceAccess(cloned[source]))
	if err != nil {
		return Access{}, err
	}
	cloned[source] = cloneSourceAccess(next)
	return Access{secrets: &accessSecrets{bySource: cloned}}, nil
}

// ForSource returns a defensive request-private copy for local or native
// attempt projection.
func (a Access) ForSource(source canonical.ToolKey) SourceAccess {
	if a.secrets == nil {
		return SourceAccess{}
	}
	return cloneSourceAccess(a.secrets.bySource[source])
}

func cloneSourceAccess(value SourceAccess) SourceAccess {
	cloned := SourceAccess{Authorization: value.Authorization}
	if value.Headers != nil {
		cloned.Headers = make(map[string]string, len(value.Headers))
		for name, headerValue := range value.Headers {
			cloned.Headers[name] = headerValue
		}
	}
	return cloned
}

func (Access) String() string   { return "<mcp-access>" }
func (Access) GoString() string { return "<mcp-access>" }
func (Access) LogValue() slog.Value {
	return slog.StringValue("<mcp-access>")
}
