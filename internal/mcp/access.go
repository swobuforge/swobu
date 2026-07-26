package mcp

import (
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// Access is request-private authorization for MCP sources. It may cross ingress
// as an opaque value; only this package can read bearer values.
type accessSecrets struct {
	bearerBySource map[canonical.ToolKey]string
}

type Access struct {
	secrets *accessSecrets
}

// WithBearer returns access containing one source credential. Contradictory
// values reject instead of depending on declaration order.
func (a Access) WithBearer(source canonical.ToolKey, bearer string) (Access, error) {
	bearer = strings.TrimSpace(bearer)
	if source.IsZero() || source.Kind() != canonical.ToolKindNamespace || bearer == "" {
		return Access{}, canonical.BadRequest("responses MCP authorization is invalid")
	}
	priorSecrets := map[canonical.ToolKey]string(nil)
	if a.secrets != nil {
		priorSecrets = a.secrets.bearerBySource
	}
	cloned := make(map[canonical.ToolKey]string, len(priorSecrets)+1)
	for key, value := range priorSecrets {
		cloned[key] = value
	}
	if prior, ok := cloned[source]; ok && prior != bearer {
		return Access{}, canonical.BadRequest("responses MCP source has contradictory bearer authorization")
	}
	cloned[source] = bearer
	return Access{secrets: &accessSecrets{bearerBySource: cloned}}, nil
}

func (a Access) bearer(source canonical.ToolKey) string {
	if a.secrets == nil {
		return ""
	}
	return a.secrets.bearerBySource[source]
}

func (Access) String() string   { return "<mcp-access>" }
func (Access) GoString() string { return "<mcp-access>" }
func (Access) LogValue() slog.Value {
	return slog.StringValue("<mcp-access>")
}
