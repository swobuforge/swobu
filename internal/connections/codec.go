package connections

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
)

// record is private persistence, never an HTTP projection. RawMessage selects
// exactly one closed kind-specific payload, avoiding irrelevant optional fields.
type record struct {
	ID          string          `json:"id"`
	DisplayName string          `json:"display_name"`
	Kind        Kind            `json:"kind"`
	Spec        json.RawMessage `json:"spec"`
	Revision    string          `json:"revision"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}
type modelRecord struct {
	Shape         string          `json:"shape"`
	Provider      string          `json:"provider"`
	Configuration json.RawMessage `json:"configuration"`
}
type standardRecord struct {
	Endpoint   string `json:"endpoint,omitempty"`
	Credential string `json:"credential_ref,omitempty"`
}
type zaiRecord struct {
	Access     string `json:"access"`
	Credential string `json:"credential_ref"`
}
type bedrockRecord struct {
	Region     string `json:"region"`
	Endpoint   string `json:"endpoint"`
	Credential string `json:"credential_ref,omitempty"`
}
type customRecord struct {
	Endpoint   string `json:"endpoint"`
	Header     string `json:"header,omitempty"`
	Credential string `json:"credential_ref,omitempty"`
}
type httpRecord struct {
	Endpoint   string `json:"endpoint"`
	Credential string `json:"credential_ref,omitempty"`
}
type stdioRecord struct {
	Executable  string            `json:"executable"`
	Args        []string          `json:"args"`
	Environment map[string]string `json:"env_refs"`
}
type mcpRecord struct {
	ExecutionIdentity string          `json:"execution_identity,omitempty"`
	PermissionSummary string          `json:"permission_summary,omitempty"`
	Transport         string          `json:"transport"`
	Configuration     json.RawMessage `json:"configuration"`
}
type searchRecord struct {
	Provider   string `json:"provider"`
	Endpoint   string `json:"endpoint"`
	Credential string `json:"credential_ref,omitempty"`
}

func strictDecode(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errors.New("invalid Connection document")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("trailing Connection document")
	}
	return nil
}
func encodeRecord(c Connection) (record, error) {
	if _, err := Restore(c.id, c.label, c.spec, c.revision, c.createdAt, c.updatedAt); err != nil {
		return record{}, err
	}
	var payload any
	switch spec := c.spec.(type) {
	case ModelSpec:
		model := modelRecord{Provider: string(spec.upstream.Provider())}
		var configuration any
		switch upstream := spec.upstream.(type) {
		case routing.StandardConnection:
			endpoint, _ := upstream.Locator()
			model.Shape = "standard"
			configuration = standardRecord{endpoint.String(), upstream.Credential().String()}
		case routing.ZAIConnection:
			model.Shape = "zai"
			configuration = zaiRecord{string(upstream.Access()), upstream.Credential().String()}
		case routing.BedrockConnection:
			model.Shape = "bedrock"
			configuration = bedrockRecord{upstream.Region().String(), upstream.Endpoint(), upstream.Credential().String()}
		case routing.CustomConnection:
			model.Shape = "custom"
			value := customRecord{Endpoint: upstream.BaseURL().String()}
			if auth, ok := upstream.Auth().(routing.CustomHeaderAuth); ok {
				value.Header = auth.Name()
				value.Credential = auth.Credential().String()
			}
			configuration = value
		default:
			return record{}, errors.New("unsupported model shape")
		}
		raw, err := json.Marshal(configuration)
		if err != nil {
			return record{}, err
		}
		model.Configuration = raw
		payload = model
	case MCPHTTPSpec:
		raw, _ := json.Marshal(httpRecord{spec.endpoint.String(), spec.credential.String()})
		payload = mcpRecord{Transport: "streamable_http", Configuration: raw, ExecutionIdentity: spec.ExecutionIdentity(), PermissionSummary: spec.PermissionSummary()}
	case MCPStdioSpec:
		environment := map[string]string{}
		for name, ref := range spec.environment {
			environment[name] = ref.String()
		}
		raw, _ := json.Marshal(stdioRecord{spec.executable, spec.Args(), environment})
		payload = mcpRecord{Transport: "stdio", Configuration: raw, ExecutionIdentity: spec.ExecutionIdentity(), PermissionSummary: spec.PermissionSummary()}
	case SearchSpec:
		payload = searchRecord{spec.provider, spec.endpoint.String(), spec.credential.String()}
	default:
		return record{}, errors.New("unsupported Connection kind")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return record{}, err
	}
	return record{c.id.String(), c.label, c.spec.Kind(), raw, c.revision.String(), c.createdAt, c.updatedAt}, nil
}

// decodeRecord requires the adapter catalog's provider admission predicate.
// Persisted provider labels cannot authorize an unsupported runtime adapter.
func decodeRecord(value record, supports routing.ProviderSupport) (Connection, error) {
	id, err := connectionidentity.Parse(value.ID)
	if err != nil {
		return Connection{}, err
	}
	revision, err := ParseRevision(value.Revision)
	if err != nil {
		return Connection{}, err
	}
	var spec Spec
	switch value.Kind {
	case Model:
		var model modelRecord
		if err := strictDecode(value.Spec, &model); err != nil {
			return Connection{}, err
		}
		provider, err := routing.ParseProvider(model.Provider, supports)
		if err != nil {
			return Connection{}, errors.New("unsupported Connection provider")
		}
		var upstream routing.Connection
		switch model.Shape {
		case "standard":
			var cfg standardRecord
			if err = strictDecode(model.Configuration, &cfg); err == nil {
				upstream, err = routing.NewStandardConnection(provider, cfg.Endpoint, cfg.Credential)
			}
		case "zai":
			var cfg zaiRecord
			if err = strictDecode(model.Configuration, &cfg); err == nil {
				upstream, err = routing.NewZAIConnection(provider, routing.ZAIAccess(cfg.Access), cfg.Credential)
			}
		case "bedrock":
			var cfg bedrockRecord
			if err = strictDecode(model.Configuration, &cfg); err == nil {
				var region routing.BedrockRegion
				region, err = routing.ParseBedrockRegion(cfg.Region)
				if err == nil {
					upstream, err = routing.NewBedrockConnection(provider, region, cfg.Endpoint, cfg.Credential)
				}
			}
		case "custom":
			var cfg customRecord
			if err = strictDecode(model.Configuration, &cfg); err == nil {
				var auth routing.CustomAuth
				if cfg.Header != "" {
					auth, err = routing.NewCustomHeaderAuth(cfg.Header, cfg.Credential)
				} else if cfg.Credential != "" {
					err = errors.New("custom credential requires header")
				}
				if err == nil {
					upstream, err = routing.NewCustomConnection(provider, cfg.Endpoint, auth)
				}
			}
		default:
			err = errors.New("unsupported model shape")
		}
		if err != nil {
			return Connection{}, errors.New("invalid model Connection configuration")
		}
		spec, err = NewModelSpec(upstream)
		if err != nil {
			return Connection{}, err
		}
	case MCP:
		var transport mcpRecord
		if err := strictDecode(value.Spec, &transport); err != nil {
			return Connection{}, err
		}
		switch transport.Transport {
		case "streamable_http":
			var cfg httpRecord
			if err := strictDecode(transport.Configuration, &cfg); err != nil {
				return Connection{}, err
			}
			endpoint, err := ParseEndpoint(cfg.Endpoint)
			if err != nil {
				return Connection{}, err
			}
			spec, err = NewMCPHTTPSpec(endpoint, credentialref.Parse(cfg.Credential))
			if err != nil {
				return Connection{}, err
			}
		case "stdio":
			var cfg stdioRecord
			if err := strictDecode(transport.Configuration, &cfg); err != nil {
				return Connection{}, err
			}
			environment := map[string]credentialref.Ref{}
			for name, ref := range cfg.Environment {
				environment[name] = credentialref.Parse(ref)
			}
			spec, err = NewMCPStdioSpec(cfg.Executable, cfg.Args, environment)
			if err != nil {
				return Connection{}, err
			}
		default:
			return Connection{}, errors.New("unsupported MCP transport")
		}
		if err != nil {
			return Connection{}, err
		}
		switch value := spec.(type) {
		case MCPHTTPSpec:
			spec, err = value.WithOperatorDeclarations(transport.ExecutionIdentity, transport.PermissionSummary)
		case MCPStdioSpec:
			spec, err = value.WithOperatorDeclarations(transport.ExecutionIdentity, transport.PermissionSummary)
		}
		if err != nil {
			return Connection{}, err
		}
	case Search:
		var cfg searchRecord
		if err := strictDecode(value.Spec, &cfg); err != nil {
			return Connection{}, err
		}
		endpoint, parseErr := ParseEndpoint(cfg.Endpoint)
		if parseErr != nil {
			return Connection{}, parseErr
		}
		spec, err = NewSearchSpec(cfg.Provider, endpoint, credentialref.Parse(cfg.Credential))
		if err != nil {
			return Connection{}, err
		}
	default:
		return Connection{}, errors.New("unsupported Connection kind")
	}
	return Restore(id, value.DisplayName, spec, revision, value.CreatedAt, value.UpdatedAt)
}
