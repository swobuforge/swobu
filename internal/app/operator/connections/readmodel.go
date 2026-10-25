// Package connections projects reusable upstream identities for operator APIs.
// Public read models exclude credential locators and command arguments; private
// Connection persistence records must never be serialized into responses.
package connections

import (
	"errors"
	"net/url"
	"path/filepath"
	"time"

	domain "github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/routing"
)

// Spec is a closed response sum. Each variant exposes only fields meaningful
// to its provider/transport shape. Request DTOs are independently validated and
// must not reuse redacted readback as editable configuration.
type Spec interface{ isSpecView() }

type StandardModel struct {
	Shape                    string `json:"shape"`
	Provider                 string `json:"provider_id"`
	Endpoint                 string `json:"endpoint,omitempty"`
	AuthenticationConfigured bool   `json:"authentication_configured"`
}

func (StandardModel) isSpecView() {}

type ZAIModel struct {
	Shape                    string `json:"shape"`
	Provider                 string `json:"provider_id"`
	Access                   string `json:"access"`
	AuthenticationConfigured bool   `json:"authentication_configured"`
}

func (ZAIModel) isSpecView() {}

type BedrockModel struct {
	Shape                    string `json:"shape"`
	Provider                 string `json:"provider_id"`
	Region                   string `json:"region"`
	Endpoint                 string `json:"endpoint"`
	AuthenticationConfigured bool   `json:"authentication_configured"`
}

func (BedrockModel) isSpecView() {}

type CustomModel struct {
	Shape                    string `json:"shape"`
	Provider                 string `json:"provider_id"`
	Endpoint                 string `json:"endpoint"`
	AuthenticationConfigured bool   `json:"authentication_configured"`
}

func (CustomModel) isSpecView() {}

// MCPHTTP exposes operator declarations as unverified claims, not tool safety.
type MCPHTTP struct {
	ExecutionIdentity        string `json:"execution_identity"`
	PermissionSummary        string `json:"permission_summary"`
	IdentityVerified         bool   `json:"identity_verified"`
	Transport                string `json:"transport"`
	Endpoint                 string `json:"endpoint"`
	AuthenticationConfigured bool   `json:"authentication_configured"`
}

func (MCPHTTP) isSpecView() {}

// MCPStdio retains unverified operator declarations without exposing argv/env.
type MCPStdio struct {
	ExecutionIdentity       string `json:"execution_identity"`
	PermissionSummary       string `json:"permission_summary"`
	IdentityVerified        bool   `json:"identity_verified"`
	Transport               string `json:"transport"`
	Executable              string `json:"executable_name"`
	ArgumentCount           int    `json:"argument_count"`
	EnvironmentBindingCount int    `json:"environment_binding_count"`
}

func (MCPStdio) isSpecView() {}

type Search struct {
	Provider                 string `json:"provider_id"`
	Endpoint                 string `json:"endpoint"`
	AuthenticationConfigured bool   `json:"authentication_configured"`
}

func (Search) isSpecView() {}

type Check struct {
	ConnectionID       string                `json:"connection_id"`
	ConnectionRevision string                `json:"connection_revision"`
	CheckedAt          time.Time             `json:"checked_at"`
	Reachability       domain.Reachability   `json:"reachability"`
	Authentication     domain.Authentication `json:"authentication"`
	Capabilities       []domain.Capability   `json:"capability_summary"`
	Error              domain.CheckError     `json:"sanitized_error,omitempty"`
}

type Connection struct {
	ID          string      `json:"id"`
	DisplayName string      `json:"display_name"`
	Kind        domain.Kind `json:"kind"`
	Spec        Spec        `json:"spec"`
	Revision    string      `json:"revision"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	LatestCheck *Check      `json:"latest_check,omitempty"`
}

// Project uses an explicitly captured observation; mismatched identity/revision
// is omitted even if a caller accidentally supplies stale health. Success is
// timestamped evidence, never a persisted readiness flag.
func Project(value domain.Connection, observation *domain.Check) (Connection, error) {
	if value.ID().String() == "" {
		return Connection{}, errors.New("validated Connection required")
	}
	var spec Spec
	switch source := value.Spec().(type) {
	case domain.ModelSpec:
		switch upstream := source.Upstream().(type) {
		case routing.StandardConnection:
			endpoint, _ := upstream.Locator()
			spec = StandardModel{"standard", string(upstream.Provider()), safeEndpoint(endpoint.String()), len(value.CredentialRefs()) != 0}
		case routing.ZAIConnection:
			spec = ZAIModel{"zai", string(upstream.Provider()), string(upstream.Access()), len(value.CredentialRefs()) != 0}
		case routing.BedrockConnection:
			spec = BedrockModel{"bedrock", string(upstream.Provider()), upstream.Region().String(), safeEndpoint(upstream.Endpoint()), len(value.CredentialRefs()) != 0}
		case routing.CustomConnection:
			spec = CustomModel{"custom", string(upstream.Provider()), safeEndpoint(upstream.BaseURL().String()), len(value.CredentialRefs()) != 0}
		default:
			return Connection{}, errors.New("unsupported model Connection shape")
		}
	case domain.MCPHTTPSpec:
		spec = MCPHTTP{Transport: "streamable_http", Endpoint: safeEndpoint(source.Endpoint().String()), AuthenticationConfigured: len(value.CredentialRefs()) != 0, ExecutionIdentity: source.ExecutionIdentity(), PermissionSummary: source.PermissionSummary()}
	case domain.MCPStdioSpec:
		spec = MCPStdio{Transport: "stdio", Executable: filepath.Base(source.Executable()), ArgumentCount: len(source.Args()), EnvironmentBindingCount: len(source.Environment()), ExecutionIdentity: source.ExecutionIdentity(), PermissionSummary: source.PermissionSummary()}
	case domain.SearchSpec:
		spec = Search{source.Provider(), safeEndpoint(source.Endpoint().String()), len(value.CredentialRefs()) != 0}
	default:
		return Connection{}, errors.New("unsupported Connection kind")
	}
	result := Connection{ID: value.ID().String(), DisplayName: value.DisplayName(), Kind: value.Spec().Kind(), Spec: spec, Revision: value.Revision().String(), CreatedAt: value.CreatedAt(), UpdatedAt: value.UpdatedAt()}
	if observation != nil && observation.ConnectionID() == value.ID() && observation.ConnectionRevision() == value.Revision() && !observation.CheckedAt().Before(value.UpdatedAt()) {
		capabilities := observation.Capabilities()
		if capabilities == nil {
			capabilities = []domain.Capability{}
		}
		result.LatestCheck = &Check{value.ID().String(), value.Revision().String(), observation.CheckedAt(), observation.Reachability(), observation.Authentication(), capabilities, observation.ErrorCode()}
	}
	return result, nil
}

func safeEndpoint(raw string) string {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return ""
	}
	endpoint.User = nil
	endpoint.RawQuery = ""
	endpoint.ForceQuery = false
	endpoint.Fragment = ""
	endpoint.RawFragment = ""
	return endpoint.String()
}
