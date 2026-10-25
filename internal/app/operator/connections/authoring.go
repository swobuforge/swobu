package connections

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	domain "github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

// Draft is explicit authoring input, never a redacted read model or private
// persistence record. Spec selects a closed kind/shape payload. Credential
// locators are write-only configuration; raw material uses replacement commands.
type Draft struct {
	DisplayName  string          `json:"display_name"`
	Kind         domain.Kind     `json:"kind"`
	Spec         json.RawMessage `json:"spec"`
	SameIdentity bool            `json:"same_identity,omitempty"`
}

func (d *Draft) UnmarshalJSON(raw []byte) error {
	type wire Draft
	var value wire
	if err := decodeDraft(raw, &value); err != nil {
		return err
	}
	*d = Draft(value)
	return nil
}

// ValidatedSpec delegates semantic validation to established owning constructors.
// The discriminator object has no optional fields from another spec variant.
func (d Draft) ValidatedSpec(supports routing.ProviderSupport) (domain.Spec, error) {
	invalid := errors.New("invalid Connection authoring spec")
	if d.DisplayName == "" || d.DisplayName != strings.TrimSpace(d.DisplayName) || !utf8.ValidString(d.DisplayName) {
		return nil, invalid
	}
	for _, char := range d.DisplayName {
		if unicode.IsControl(char) {
			return nil, invalid
		}
	}

	switch d.Kind {
	case domain.Model:
		var model struct {
			Shape         string          `json:"shape"`
			Provider      string          `json:"provider_id"`
			Configuration json.RawMessage `json:"configuration"`
		}
		if decodeDraft(d.Spec, &model) != nil {
			return nil, invalid
		}
		provider, err := routing.ParseProvider(model.Provider, supports)
		if err != nil {
			return nil, invalid
		}
		var upstream routing.Connection
		shape, ok := profile.ConnectionShapeForSpec(model.Provider)
		if !ok {
			return nil, invalid
		}
		expectedShapes := map[string]routing.ConnectionShape{"standard": routing.ConnectionShapeStandard, "zai": routing.ConnectionShapeZAI, "bedrock": routing.ConnectionShapeBedrock, "custom": routing.ConnectionShapeCustom}
		if expectedShapes[model.Shape] != shape {
			return nil, invalid
		}
		switch model.Shape {
		case "standard":
			var cfg struct {
				Endpoint   string `json:"endpoint,omitempty"`
				Credential string `json:"credential_ref"`
			}
			if decodeDraft(model.Configuration, &cfg) != nil {
				return nil, invalid
			}
			if cfg.Endpoint != "" {
				if _, err := domain.ParseEndpoint(cfg.Endpoint); err != nil {
					return nil, invalid
				}
			}
			validated, validateErr := profile.ValidateStandardConnection(provider, routing.StandardConnectionDraft{Locator: cfg.Endpoint, Credential: cfg.Credential})
			if validateErr != nil {
				return nil, invalid
			}
			upstream, err = routing.NewStandardConnection(provider, validated.Locator, validated.Credential)
		case "zai":
			var cfg struct {
				Access     string `json:"access"`
				Credential string `json:"credential_ref"`
			}
			if decodeDraft(model.Configuration, &cfg) != nil {
				return nil, invalid
			}
			upstream, err = routing.NewZAIConnection(provider, routing.ZAIAccess(cfg.Access), cfg.Credential)
		case "bedrock":
			var cfg struct {
				Region     string `json:"region"`
				Endpoint   string `json:"endpoint,omitempty"`
				Credential string `json:"credential_ref,omitempty"`
			}
			if decodeDraft(model.Configuration, &cfg) != nil {
				return nil, invalid
			}
			if _, err := domain.ParseEndpoint(cfg.Endpoint); err != nil {
				return nil, invalid
			}
			region, regionErr := routing.ParseBedrockRegion(cfg.Region)
			if regionErr != nil || !profile.SupportsBedrockMantleRegion(cfg.Region) {
				return nil, invalid
			}
			// Connection authoring precedes target protocol selection. Validate
			// provider-owned endpoint/region coherence without choosing a protocol
			// or rewriting stored identity during legacy decoding.
			resolved, resolveErr := profile.ResolveBedrockEndpoint(cfg.Endpoint, region.String(), "")
			if resolveErr != nil {
				return nil, invalid
			}
			cfg.Endpoint = resolved.BaseURL
			upstream, err = routing.NewBedrockConnection(provider, region, cfg.Endpoint, cfg.Credential)
		case "custom":
			var cfg struct {
				Endpoint   string `json:"endpoint"`
				Header     string `json:"header,omitempty"`
				Credential string `json:"credential_ref,omitempty"`
			}
			if decodeDraft(model.Configuration, &cfg) != nil {
				return nil, invalid
			}
			if _, err := domain.ParseEndpoint(cfg.Endpoint); err != nil {
				return nil, invalid
			}
			var auth routing.CustomAuth
			if cfg.Header != "" {
				auth, err = routing.NewCustomHeaderAuth(cfg.Header, cfg.Credential)
			} else if cfg.Credential != "" {
				return nil, invalid
			}
			if err == nil {
				upstream, err = routing.NewCustomConnection(provider, cfg.Endpoint, auth)
			}
		default:
			return nil, invalid
		}
		if err != nil {
			return nil, invalid
		}
		return domain.NewModelSpec(upstream)
	case domain.MCP:
		var mcp struct {
			ExecutionIdentity string          `json:"execution_identity,omitempty"`
			PermissionSummary string          `json:"permission_summary,omitempty"`
			Transport         string          `json:"transport"`
			Configuration     json.RawMessage `json:"configuration"`
		}
		if decodeDraft(d.Spec, &mcp) != nil {
			return nil, invalid
		}
		switch mcp.Transport {
		case "streamable_http":
			var cfg struct {
				Endpoint   string `json:"endpoint"`
				Credential string `json:"credential_ref,omitempty"`
			}
			if decodeDraft(mcp.Configuration, &cfg) != nil {
				return nil, invalid
			}
			endpoint, err := domain.ParseEndpoint(cfg.Endpoint)
			if err != nil {
				return nil, invalid
			}
			spec, err := domain.NewMCPHTTPSpec(endpoint, credentialref.Parse(cfg.Credential))
			if err != nil {
				return nil, err
			}
			return spec.WithOperatorDeclarations(mcp.ExecutionIdentity, mcp.PermissionSummary)
		case "stdio":
			var cfg struct {
				Executable  string            `json:"executable"`
				Args        []string          `json:"args"`
				Environment map[string]string `json:"env_refs"`
			}
			if decodeDraft(mcp.Configuration, &cfg) != nil {
				return nil, invalid
			}
			environment := map[string]credentialref.Ref{}
			for key, value := range cfg.Environment {
				environment[key] = credentialref.Parse(value)
			}
			spec, err := domain.NewMCPStdioSpec(cfg.Executable, cfg.Args, environment)
			if err != nil {
				return nil, err
			}
			return spec.WithOperatorDeclarations(mcp.ExecutionIdentity, mcp.PermissionSummary)
		default:
			return nil, invalid
		}
	case domain.Search:
		var cfg struct {
			Provider   string `json:"provider_id"`
			Endpoint   string `json:"endpoint"`
			Credential string `json:"credential_ref,omitempty"`
		}
		if decodeDraft(d.Spec, &cfg) != nil {
			return nil, invalid
		}
		endpoint, err := domain.ParseEndpoint(cfg.Endpoint)
		if err != nil {
			return nil, invalid
		}
		return domain.NewSearchSpec(cfg.Provider, endpoint, credentialref.Parse(cfg.Credential))
	default:
		return nil, invalid
	}
}

func decodeDraft(raw []byte, target any) error {
	// Reject duplicate and null members recursively before decoding typed fields.
	tokens := json.NewDecoder(bytes.NewReader(raw))
	var consume func() error
	consume = func() error {
		token, err := tokens.Token()
		if err != nil {
			return err
		}
		if token == nil {
			return errors.New("null authoring field")
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		seen := map[string]bool{}
		for tokens.More() {
			if delimiter == '{' {
				name, err := tokens.Token()
				if err != nil {
					return err
				}
				key, ok := name.(string)
				if !ok || seen[key] {
					return errors.New("duplicate authoring field")
				}
				seen[key] = true
			}
			if err := consume(); err != nil {
				return err
			}
		}
		_, err = tokens.Token()
		return err
	}
	if err := consume(); err != nil {
		return err
	}
	if _, err := tokens.Token(); err != io.EOF {
		return errors.New("trailing authoring input")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("authoring object required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
