// Package workspaces owns operator queries and semantic commands over the
// latest routing aggregate. It converts transport drafts to routing.TargetDraft
// without interpreting providers and never accepts caller-authored snapshots.
package workspaces

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

type Workspace struct {
	Slug         string  `json:"slug"`
	DefaultRoute string  `json:"default_route"`
	Routes       []Route `json:"routes"`
}
type WorkspaceSummary struct {
	Slug         string `json:"slug"`
	DefaultRoute string `json:"default_route"`
	RouteCount   int    `json:"route_count"`
}
type Route struct {
	Name  string `json:"name"`
	Tiers []Tier `json:"tiers"`
}
type Tier struct {
	Targets []Target `json:"targets"`
}
type Target struct {
	ID         string     `json:"id"`
	Model      string     `json:"model"`
	Protocol   string     `json:"protocol"`
	Provider   string     `json:"provider"`
	Connection Connection `json:"connection"`
}

// TargetDraft is create-command input. Provider identity is derived solely
// from the one populated provider-keyed connection document.
type TargetDraft struct {
	ID         string     `json:"id"`
	Model      string     `json:"model"`
	Protocol   string     `json:"protocol"`
	Connection Connection `json:"connection"`
}

// TargetSettingsDraft is update-command input; the request path is the only
// stable target identity source.
type TargetSettingsDraft struct {
	Model      string     `json:"model"`
	Protocol   string     `json:"protocol"`
	Connection Connection `json:"connection"`
}

// Connection preserves the public provider-keyed JSON shape while storing a
// shape-oriented routing draft. Profile is the sole catalog-membership owner;
// unknown keys and fields fail at this boundary rather than becoming maps in
// routing state.
type Connection struct{ draft routing.ConnectionDraft }

type standardConnectionDTO struct {
	BaseURL    string `json:"base_url,omitempty"`
	Credential string `json:"credential,omitempty"`
}
type ZAIConnection struct {
	Access     string `json:"access"`
	Credential string `json:"credential"`
}
type azureProjectConnectionDTO struct {
	ProjectEndpoint string `json:"project_endpoint"`
	Credential      string `json:"credential"`
}
type BedrockConnection struct {
	Region     string `json:"region"`
	Endpoint   string `json:"endpoint,omitempty"`
	Credential string `json:"credential,omitempty"`
}
type CustomConnection struct {
	BaseURL string        `json:"base_url"`
	Header  *CustomHeader `json:"header,omitempty"`
}
type CustomHeader struct {
	Name       string `json:"name"`
	Credential string `json:"credential"`
}

// StandardConnection returns a JSON connection document for a Standard-shape
// provider. It is a construction helper for internal callers and tests; JSON
// marshal still emits the existing provider-keyed public contract.
func StandardConnection(provider, locator, credential string) Connection {
	return Connection{draft: routing.ConnectionDraft{Provider: provider, Standard: &routing.StandardConnectionDraft{Locator: locator, Credential: credential}}}
}

// ZAIConnectionDocument returns a Z.AI provider-keyed connection document.
func ZAIConnectionDocument(access, credential string) Connection {
	return Connection{draft: routing.ConnectionDraft{Provider: string(profile.ProviderSpecZAI), ZAI: &routing.ZAIConnectionDraft{Access: access, Credential: credential}}}
}

// BedrockConnectionDocument returns a Bedrock provider-keyed connection
// document. It permits an empty endpoint for the catalog-only probe flow;
// durable target finalization still rejects it.
func BedrockConnectionDocument(region, endpoint, credential string) Connection {
	return Connection{draft: routing.ConnectionDraft{Provider: string(profile.ProviderSpecBedrock), Bedrock: &routing.BedrockConnectionDraft{Region: region, Endpoint: endpoint, Credential: credential}}}
}

// CustomConnectionDocument returns a custom-endpoint provider-keyed connection
// document with the optional user-authored header behavior.
func CustomConnectionDocument(baseURL string, header *CustomHeader) Connection {
	draft := routing.ConnectionDraft{Provider: string(profile.ProviderSpecCustom), Custom: &routing.CustomConnectionDraft{BaseURL: baseURL}}
	if header != nil {
		draft.Custom.Header = &routing.CustomHeaderDraft{Name: header.Name, Credential: header.Credential}
	}
	return Connection{draft: draft}
}

// BedrockDraft returns the boundary-only Bedrock fields for the one special
// catalog probe that can run before an operator supplies an inference endpoint.
func (c Connection) BedrockDraft() (BedrockConnection, bool) {
	if c.draft.Provider != string(profile.ProviderSpecBedrock) || c.draft.Bedrock == nil {
		return BedrockConnection{}, false
	}
	return BedrockConnection{Region: c.draft.Bedrock.Region, Endpoint: c.draft.Bedrock.Endpoint, Credential: c.draft.Bedrock.Credential}, true
}

func (c Connection) MarshalJSON() ([]byte, error) {
	provider := strings.TrimSpace(c.draft.Provider)
	shape, ok := profile.ConnectionShapeForSpec(provider)
	if !ok {
		return nil, fmt.Errorf("connection.%s: provider is unsupported", provider)
	}
	switch shape {
	case routing.ConnectionShapeStandard:
		if c.draft.Standard == nil {
			return nil, fmt.Errorf("connection.%s: provider connection details are required", provider)
		}
		locator, _ := profile.LocatorSpecForProvider(provider)
		if locator.Kind == profile.LocatorAzureProject {
			return json.Marshal(map[string]any{provider: azureProjectConnectionDTO{ProjectEndpoint: c.draft.Standard.Locator, Credential: c.draft.Standard.Credential}})
		}
		if locator.Kind == profile.LocatorFixed {
			return json.Marshal(map[string]any{provider: struct {
				Credential string `json:"credential"`
			}{Credential: c.draft.Standard.Credential}})
		}
		return json.Marshal(map[string]any{provider: standardConnectionDTO{BaseURL: c.draft.Standard.Locator, Credential: c.draft.Standard.Credential}})
	case routing.ConnectionShapeZAI:
		if c.draft.ZAI == nil {
			return nil, fmt.Errorf("connection.%s: Z.AI payload is required", provider)
		}
		return json.Marshal(map[string]any{provider: ZAIConnection{Access: c.draft.ZAI.Access, Credential: c.draft.ZAI.Credential}})
	case routing.ConnectionShapeBedrock:
		if c.draft.Bedrock == nil {
			return nil, fmt.Errorf("connection.%s: Bedrock payload is required", provider)
		}
		return json.Marshal(map[string]any{provider: BedrockConnection{Region: c.draft.Bedrock.Region, Endpoint: c.draft.Bedrock.Endpoint, Credential: c.draft.Bedrock.Credential}})
	case routing.ConnectionShapeCustom:
		if c.draft.Custom == nil {
			return nil, fmt.Errorf("connection.%s: custom payload is required", provider)
		}
		encoded := CustomConnection{BaseURL: c.draft.Custom.BaseURL}
		if header := c.draft.Custom.Header; header != nil {
			encoded.Header = &CustomHeader{Name: header.Name, Credential: header.Credential}
		}
		return json.Marshal(map[string]any{provider: encoded})
	default:
		return nil, fmt.Errorf("connection.%s: provider connection requirements are unsupported", provider)
	}
}

func (c *Connection) UnmarshalJSON(raw []byte) error {
	provider, payload, err := singleProviderJSON(raw)
	if err != nil {
		return err
	}
	shape, ok := profile.ConnectionShapeForSpec(provider)
	if !ok {
		return fmt.Errorf("connection.%s: provider is unsupported", provider)
	}
	draft := routing.ConnectionDraft{Provider: provider}
	switch shape {
	case routing.ConnectionShapeStandard:
		locator, _ := profile.LocatorSpecForProvider(provider)
		entry, _ := profile.ProfileForSpec(provider)
		if locator.Kind == profile.LocatorAzureProject {
			var encoded azureProjectConnectionDTO
			if err := decodeStrictJSONObject(payload, &encoded); err != nil {
				return fmt.Errorf("connection.%s: %w", provider, err)
			}
			if err := validateStandardConnectionJSONKeys(payload, entry); err != nil {
				return err
			}
			draft.Standard = &routing.StandardConnectionDraft{Locator: encoded.ProjectEndpoint, Credential: encoded.Credential}
		} else {
			var encoded standardConnectionDTO
			if err := decodeStrictJSONObject(payload, &encoded); err != nil {
				return fmt.Errorf("connection.%s: %w", provider, err)
			}
			if err := validateStandardConnectionJSONKeys(payload, entry); err != nil {
				return err
			}
			draft.Standard = &routing.StandardConnectionDraft{Locator: encoded.BaseURL, Credential: encoded.Credential}
		}
	case routing.ConnectionShapeZAI:
		var encoded ZAIConnection
		if err := decodeStrictJSONObject(payload, &encoded); err != nil {
			return fmt.Errorf("connection.%s: %w", provider, err)
		}
		draft.ZAI = &routing.ZAIConnectionDraft{Access: encoded.Access, Credential: encoded.Credential}
	case routing.ConnectionShapeBedrock:
		var encoded BedrockConnection
		if err := decodeStrictJSONObject(payload, &encoded); err != nil {
			return fmt.Errorf("connection.%s: %w", provider, err)
		}
		draft.Bedrock = &routing.BedrockConnectionDraft{Region: encoded.Region, Endpoint: encoded.Endpoint, Credential: encoded.Credential}
	case routing.ConnectionShapeCustom:
		var encoded CustomConnection
		if err := decodeStrictJSONObject(payload, &encoded); err != nil {
			return fmt.Errorf("connection.%s: %w", provider, err)
		}
		draft.Custom = &routing.CustomConnectionDraft{BaseURL: encoded.BaseURL}
		if encoded.Header != nil {
			draft.Custom.Header = &routing.CustomHeaderDraft{Name: encoded.Header.Name, Credential: encoded.Header.Credential}
		}
	default:
		return fmt.Errorf("connection.%s: provider connection requirements are unsupported", provider)
	}
	c.draft = draft
	return nil
}

func singleProviderJSON(raw []byte) (string, json.RawMessage, error) {
	var object map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&object); err != nil {
		return "", nil, errors.New("connection must be a JSON object")
	}
	if len(object) != 1 {
		return "", nil, errors.New("connection must contain exactly one provider key")
	}
	for provider, payload := range object {
		if !profile.SupportsSpec(provider) {
			return "", nil, fmt.Errorf("connection.%s: provider is unsupported", provider)
		}
		return provider, payload, nil
	}
	panic("one-key map had no key")
}

func decodeStrictJSONObject(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("connection payload has trailing data")
	}
	return nil
}

// validateStandardConnectionJSONKeys preserves strict public grammar by
// inspecting authored keys, not their decoded values. In particular, an empty
// fixed-provider base_url is still an unrecognized public field.
func validateStandardConnectionJSONKeys(raw []byte, entry profile.Profile) error {
	provider := string(entry.ProviderID)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("connection.%s: connection payload must be a JSON object", provider)
	}
	if _, present := fields["base_url"]; present && entry.Locator.Kind != profile.LocatorBaseURL {
		return fmt.Errorf("connection.%s: field \"base_url\" is not recognized", provider)
	}
	if _, present := fields["credential"]; present && entry.Credential.Requirement == profile.CredentialUnsupported {
		return fmt.Errorf("connection.%s: field \"credential\" is not recognized", provider)
	}
	return nil
}

func projectWorkspace(workspace routing.Workspace) Workspace {
	out := Workspace{Slug: workspace.Slug().String(), DefaultRoute: workspace.DefaultRoute().String()}
	for _, route := range workspace.Routes() {
		projected := Route{Name: route.Name().String()}
		for _, tier := range route.Tiers() {
			projectedTier := Tier{}
			for _, target := range tier.Targets() {
				projectedTier.Targets = append(projectedTier.Targets, projectTarget(target))
			}
			sort.Slice(projectedTier.Targets, func(i, j int) bool { return projectedTier.Targets[i].ID < projectedTier.Targets[j].ID })
			projected.Tiers = append(projected.Tiers, projectedTier)
		}
		out.Routes = append(out.Routes, projected)
	}
	sort.Slice(out.Routes, func(i, j int) bool { return out.Routes[i].Name < out.Routes[j].Name })
	return out
}

func projectTarget(target routing.Target) Target {
	// Read models expose effective runtime truth. Single-protocol providers omit
	// protocol from authoring and persisted config, but diagnostics and external
	// operator clients still receive the effective routed protocol here.
	out := Target{ID: target.ID().String(), Model: target.Model().String(), Protocol: target.Protocol().String(), Provider: string(target.Provider())}
	out.Connection = ConnectionFromRouting(target.Connection())
	return out
}

// ConnectionFromRouting is the shared routing-to-operator transport codec used
// by both target persistence and target probing.
func ConnectionFromRouting(connection routing.Connection) Connection {
	draft, err := connectionDraftFromRouting(connection)
	if err != nil {
		panic(err)
	}
	return Connection{draft: draft}
}

func (t TargetDraft) routingTarget() (routing.Target, error) {
	return finalizeTargetDraft(t.ID, t.Model, t.Protocol, t.Connection)
}

func (t TargetSettingsDraft) routingTarget(id string) (routing.Target, error) {
	return finalizeTargetDraft(id, t.Model, t.Protocol, t.Connection)
}

func finalizeTargetDraft(id, model, protocol string, connection Connection) (routing.Target, error) {
	draft, err := connection.routingDraft()
	if err != nil {
		return routing.Target{}, err
	}
	return routing.FinalizeTarget(routing.TargetDraft{
		ID: id, Model: model, Protocol: protocol, Connection: draft,
	}, profile.RoutingConstructionFacts())
}

func (c Connection) routingDraft() (routing.ConnectionDraft, error) {
	return c.draft, nil
}

func connectionDraftFromRouting(connection routing.Connection) (routing.ConnectionDraft, error) {
	draft := routing.ConnectionDraft{Provider: string(connection.Provider())}
	switch connection := connection.(type) {
	case routing.StandardConnection:
		locator, _ := connection.Locator()
		draft.Standard = &routing.StandardConnectionDraft{Locator: locator.String(), Credential: connection.Credential().String()}
	case routing.ZAIConnection:
		draft.ZAI = &routing.ZAIConnectionDraft{Access: string(connection.Access()), Credential: connection.Credential().String()}
	case routing.BedrockConnection:
		draft.Bedrock = &routing.BedrockConnectionDraft{Region: connection.Region().String(), Endpoint: connection.Endpoint(), Credential: connection.Credential().String()}
	case routing.CustomConnection:
		draft.Custom = &routing.CustomConnectionDraft{BaseURL: connection.BaseURL().String()}
		if auth := connection.Auth(); auth != nil {
			header, ok := auth.(routing.CustomHeaderAuth)
			if !ok {
				return routing.ConnectionDraft{}, fmt.Errorf("unsupported custom auth %T", auth)
			}
			draft.Custom.Header = &routing.CustomHeaderDraft{Name: header.Name(), Credential: header.Credential().String()}
		}
	default:
		return routing.ConnectionDraft{}, fmt.Errorf("unsupported routing connection %T", connection)
	}
	return draft, nil
}

// RoutingConnection parses the operator connection union through the routing
// domain's single connection finalizer.
func (c Connection) RoutingConnection() (routing.Connection, error) {
	draft, err := c.routingDraft()
	if err != nil {
		return nil, err
	}
	return routing.FinalizeConnection(draft, profile.RoutingConstructionFacts())
}

func normalize(raw string) string { return strings.TrimSpace(raw) }
