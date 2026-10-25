package configstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

type documentDTO struct {
	// Connections are owned and validated by connections.Store, not routing.
	// Keeping the YAML node here permits strict top-level field validation.
	Connections       *yaml.Node                        `yaml:"connections,omitempty"`
	Principals        *yaml.Node                        `yaml:"principals,omitempty"`
	Publications      *yaml.Node                        `yaml:"publications,omitempty"`
	Accounting        *yaml.Node                        `yaml:"accounting,omitempty"`
	DisplayNameAudits []WorkspaceDisplayNameAudit       `yaml:"workspace_display_name_audits,omitempty"`
	DeletionAudits    map[string]WorkspaceDeletionAudit `yaml:"workspace_deletion_audits,omitempty"`
	CreationAudits    map[string]WorkspaceCreationAudit `yaml:"workspace_creation_audits,omitempty"`
	Creations         map[string]creationReceipt        `yaml:"workspace_creations,omitempty"`
	Deletions         map[string]deletionReceipt        `yaml:"workspace_deletions,omitempty"`
	SchemaVersion     int                               `yaml:"schema_version"`
	Workspaces        map[string]workspaceDTO           `yaml:"workspaces"`
}
type workspaceDTO struct {
	DisplayName           string                 `yaml:"display_name,omitempty"`
	EnabledServices       *[]string              `yaml:"enabled_services,omitempty"`
	ServiceLimits         *routing.ServiceLimits `yaml:"service_limits,omitempty"`
	ManagementIncarnation string                 `yaml:"management_incarnation,omitempty"`
	ManagementRevision    *uint64                `yaml:"management_revision,omitempty"`
	TargetGenerations     map[string]uint64      `yaml:"target_generations,omitempty"`
	Routes                []routeDTO             `yaml:"routes"`
	MCPMounts             map[string]mcpMountDTO `yaml:"mcp_mounts,omitempty"`
	Search                *searchDTO             `yaml:"search,omitempty"`
	Media                 *mediaDTO              `yaml:"media,omitempty"`
}
type mcpMountDTO struct {
	Builtin      string   `yaml:"builtin,omitempty"`
	ConnectionID string   `yaml:"connection_id,omitempty"`
	AllowedTools []string `yaml:"allowed_tools,omitempty"`
}
type searchDTO struct {
	ConnectionID          string   `yaml:"connection_id"`
	FallbackConnectionIDs []string `yaml:"fallback_connection_ids,omitempty"`
}
type mediaDTO struct {
	Routes []string `yaml:"routes,omitempty"`
}
type routeDTO struct {
	Pattern string    `yaml:"pattern"`
	Tiers   []tierDTO `yaml:"tiers"`
}
type tierDTO struct {
	Targets []targetDTO `yaml:"targets"`
}
type targetDTO struct {
	ID           string  `yaml:"id"`
	Version      *uint64 `yaml:"version"`
	Model        string  `yaml:"model"`
	Protocol     string  `yaml:"protocol"`
	ConnectionID string  `yaml:"connection_id"`
}

func decode(raw []byte) (routing.Config, error) {
	if _, err := decodeDisplayNameAudits(raw); err != nil {
		return routing.Config{}, err
	}
	if _, err := decodeWorkspaceDeletionAudits(raw); err != nil {
		return routing.Config{}, err
	}
	if _, err := decodeWorkspaceCreationAudits(raw); err != nil {
		return routing.Config{}, err
	}
	if _, err := decodeCreationReceipts(raw); err != nil {
		return routing.Config{}, err
	}
	if _, err := decodeDeletionReceipts(raw); err != nil {
		return routing.Config{}, err
	}
	// The inline Connection document is owned by the Connections codec. Decode
	// routing with strict fields after removing that opaque subtree; otherwise
	// yaml.v3 applies KnownFields recursively to the Connection schema and
	// rejects its deliberately independent fields.
	routingRaw, err := withoutNodeOwnedSections(raw)
	if err != nil {
		return routing.Config{}, fmt.Errorf("decode routing config: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(routingRaw))
	decoder.KnownFields(true)
	var dto documentDTO
	if err := decoder.Decode(&dto); err != nil {
		return routing.Config{}, fmt.Errorf("decode routing config: %w", readableDecodeError(err))
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return routing.Config{}, fmt.Errorf("decode routing config: multiple YAML documents are unsupported")
		}
		return routing.Config{}, fmt.Errorf("decode routing config: %w", err)
	}
	if dto.SchemaVersion != routing.ReferenceSchemaVersion {
		return routing.Config{}, fmt.Errorf("unsupported schema_version %d (current: %d); create a new current routing config", dto.SchemaVersion, routing.ReferenceSchemaVersion)
	}
	workspaces := make([]routing.Workspace, 0, len(dto.Workspaces))
	for rawSlug, encodedWorkspace := range dto.Workspaces {
		if encodedWorkspace.Media != nil {
			return routing.Config{}, fmt.Errorf("workspaces.%s: unsupported composed capability", rawSlug)
		}
		slug, err := routing.ParseWorkspaceSlug(rawSlug)
		if err != nil {
			return routing.Config{}, prefixPath(err, "workspaces."+rawSlug)
		}
		// Routes are one ordered value: their position is their first-match priority.
		// Mapping-form routes and separate order metadata have no meaning.
		routes := make([]routing.Route, 0, len(encodedWorkspace.Routes))
		for routeIndex, encodedRoute := range encodedWorkspace.Routes {
			rawName := encodedRoute.Pattern
			name, err := routing.ParseRouteName(rawName)
			if err != nil {
				return routing.Config{}, prefixPath(err, fmt.Sprintf("workspaces.%s.routes[%d]", rawSlug, routeIndex))
			}
			tiers := make([]routing.Tier, 0, len(encodedRoute.Tiers))
			for tierIndex, encodedTier := range encodedRoute.Tiers {
				targets := make([]routing.Target, 0, len(encodedTier.Targets))
				for targetIndex, encodedTarget := range encodedTier.Targets {
					path := fmt.Sprintf("workspaces.%s.routes[%d].tiers[%d].targets[%d]", rawSlug, routeIndex, tierIndex, targetIndex)
					target, err := decodeTarget(encodedTarget)
					if err != nil {
						return routing.Config{}, prefixPath(err, path)
					}
					targets = append(targets, target)
				}
				tier, err := routing.NewTier(targets)
				if err != nil {
					return routing.Config{}, prefixPath(err, fmt.Sprintf("workspaces.%s.routes[%d].tiers[%d]", rawSlug, routeIndex, tierIndex))
				}
				tiers = append(tiers, tier)
			}
			route, err := routing.NewRoute(name, tiers)
			if err != nil {
				return routing.Config{}, prefixPath(err, fmt.Sprintf("workspaces.%s.routes[%d]", rawSlug, routeIndex))
			}
			routes = append(routes, route)
		}
		generations := make(map[routing.TargetID]routing.TargetVersion, len(encodedWorkspace.TargetGenerations))
		for rawID, rawVersion := range encodedWorkspace.TargetGenerations {
			id, err := routing.ParseTargetID(rawID)
			if err != nil {
				return routing.Config{}, prefixPath(err, "workspaces."+rawSlug+".target_generations."+rawID)
			}
			if rawVersion == 0 {
				return routing.Config{}, fmt.Errorf("workspaces.%s.target_generations.%s: generation must be greater than zero", rawSlug, rawID)
			}
			generations[id] = routing.TargetVersion(rawVersion)
		}
		mounts := make(map[string]routing.MCPMount, len(encodedWorkspace.MCPMounts))
		for alias, source := range encodedWorkspace.MCPMounts {
			mounts[alias] = routing.MCPMount{Builtin: source.Builtin, ConnectionID: source.ConnectionID, AllowedTools: append([]string(nil), source.AllowedTools...)}
		}
		searchConnection := ""
		if encodedWorkspace.Search != nil {
			searchConnection = strings.TrimSpace(encodedWorkspace.Search.ConnectionID)
		}
		fallback := []string(nil)
		if encodedWorkspace.Search != nil {
			fallback = encodedWorkspace.Search.FallbackConnectionIDs
		}
		workspace, err := routing.RestoreWorkspaceWithSearchRoute(slug, routes, generations, mounts, searchConnection, fallback)
		if err == nil && encodedWorkspace.EnabledServices != nil {
			kinds := make([]routing.ServiceKind, 0, len(*encodedWorkspace.EnabledServices))
			for _, rawKind := range *encodedWorkspace.EnabledServices {
				kind, parseErr := routing.ParseServiceKind(rawKind)
				if parseErr != nil {
					return routing.Config{}, prefixPath(parseErr, "workspaces."+rawSlug+".enabled_services")
				}
				kinds = append(kinds, kind)
			}
			workspace, err = workspace.WithServices(kinds)
		}
		if err != nil {
			return routing.Config{}, prefixPath(err, "workspaces."+rawSlug)
		}
		if err == nil && encodedWorkspace.ServiceLimits != nil {
			workspace, err = workspace.WithServiceLimits(*encodedWorkspace.ServiceLimits)
		}
		if err != nil {
			return routing.Config{}, prefixPath(err, "workspaces."+rawSlug+".service_limits")
		}
		workspace, err = workspace.WithDisplayName(encodedWorkspace.DisplayName)
		if err != nil {
			return routing.Config{}, prefixPath(err, "workspaces."+rawSlug)
		}
		revision := uint64(1)
		if encodedWorkspace.ManagementRevision != nil {
			revision = *encodedWorkspace.ManagementRevision
			if revision == 0 {
				return routing.Config{}, fmt.Errorf("workspaces.%s.management_revision: must be greater than zero", rawSlug)
			}
		}
		workspace = workspace.WithManagementRevision(revision)
		incarnation := encodedWorkspace.ManagementIncarnation
		if incarnation != "" {
			decoded, err := hex.DecodeString(incarnation)
			if err != nil || len(decoded) != 32 || incarnation != strings.ToLower(incarnation) {
				return routing.Config{}, fmt.Errorf("workspaces.%s.management_incarnation: expected 64 lowercase hex characters", rawSlug)
			}
		} else {
			// Manually authored files may lack lifetime identity. Derive their initial value
			// from canonical persisted facts without rewriting on read. The next
			// store commit persists it; new creations always receive random IDs.
			one, err := routing.NewConfig([]routing.Workspace{workspace})
			if err != nil {
				return routing.Config{}, err
			}
			encoded, err := encodeDocument(one, dto.SchemaVersion)
			if err != nil {
				return routing.Config{}, err
			}
			digest := sha256.Sum256(append([]byte("swobu-workspace-legacy-incarnation:"), encoded...))
			incarnation = hex.EncodeToString(digest[:])
		}
		workspace = workspace.WithManagementIncarnation(incarnation)
		workspaces = append(workspaces, workspace)
	}
	return routing.NewConfig(workspaces)
}

func withoutNodeOwnedSections(raw []byte) ([]byte, error) {
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple YAML documents are unsupported")
		}
		return nil, err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("routing config must be a mapping")
	}
	root := document.Content[0]
	filtered := make([]*yaml.Node, 0, len(root.Content))
	for i := 0; i+1 < len(root.Content); i += 2 {
		switch root.Content[i].Value {
		case "connections", "principals", "publications", "accounting":
			continue
		}
		filtered = append(filtered, root.Content[i], root.Content[i+1])
	}
	root.Content = filtered
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func documentSchema(raw []byte) (int, error) {
	var header struct {
		SchemaVersion int `yaml:"schema_version"`
	}
	if err := yaml.Unmarshal(raw, &header); err != nil {
		return 0, err
	}
	return header.SchemaVersion, nil
}

func decodeTarget(dto targetDTO) (routing.Target, error) {
	target, err := routing.FinalizeTarget(routing.TargetDraft{ID: dto.ID, Model: dto.Model, Protocol: dto.Protocol, ConnectionID: dto.ConnectionID}, profile.RoutingConstructionFacts())
	if err != nil {
		return routing.Target{}, err
	}
	if dto.Version == nil || *dto.Version == 0 {
		return routing.Target{}, fmt.Errorf("positive explicit target version required")
	}
	return routing.RestoreTarget(target.ID(), routing.TargetVersion(*dto.Version), target.Model(), target.Protocol(), target.ConnectionID())
}

func encode(config routing.Config) ([]byte, error) {
	return encodeDocument(config, routing.ReferenceSchemaVersion)
}

func encodeDocument(config routing.Config, schemaVersion int) ([]byte, error) {
	return encodeDocumentWithReceipts(config, schemaVersion, nil, nil, nil, nil, nil)
}

func encodeDocumentWithReceipts(config routing.Config, schemaVersion int, receipts map[string]deletionReceipt, creations map[string]creationReceipt, audits map[string]WorkspaceCreationAudit, deletionAudits map[string]WorkspaceDeletionAudit, displayAudits []WorkspaceDisplayNameAudit) ([]byte, error) {
	dto := documentDTO{SchemaVersion: schemaVersion, Workspaces: map[string]workspaceDTO{}, Deletions: receipts, Creations: creations, CreationAudits: audits, DeletionAudits: deletionAudits, DisplayNameAudits: displayAudits}
	for _, workspace := range config.Workspaces() {
		encodedWorkspace := workspaceDTO{TargetGenerations: map[string]uint64{}, Routes: []routeDTO{}}
		encodedWorkspace.DisplayName = workspace.DisplayName()
		if limits := workspace.ServiceLimits(); limits != (routing.ServiceLimits{}) {
			encodedWorkspace.ServiceLimits = &limits
		}
		kinds := make([]string, 0, len(workspace.EnabledServices()))
		for _, kind := range workspace.EnabledServices() {
			kinds = append(kinds, string(kind))
		}
		encodedWorkspace.EnabledServices = &kinds
		encodedWorkspace.ManagementIncarnation = workspace.ManagementIncarnation()
		if revision := workspace.ManagementRevision(); revision != 0 {
			encodedWorkspace.ManagementRevision = &revision
		}
		encodedWorkspace.MCPMounts = map[string]mcpMountDTO{}
		for alias, mount := range workspace.MCPMounts() {
			encodedWorkspace.MCPMounts[alias] = mcpMountDTO{Builtin: mount.Builtin, ConnectionID: mount.ConnectionID, AllowedTools: append([]string(nil), mount.AllowedTools...)}
		}
		if sourceID := workspace.SearchConnectionID(); sourceID != "" {
			encodedWorkspace.Search = &searchDTO{ConnectionID: sourceID, FallbackConnectionIDs: workspace.SearchFallbackIDs()}
		}
		for id, version := range workspace.TargetGenerations() {
			encodedWorkspace.TargetGenerations[id.String()] = uint64(version)
		}
		for _, route := range workspace.Routes() {
			encodedRoute := routeDTO{Pattern: route.Name().String(), Tiers: make([]tierDTO, 0, len(route.Tiers()))}
			for _, tier := range route.Tiers() {
				targets := tier.Targets()
				sort.Slice(targets, func(i, j int) bool { return targets[i].ID().String() < targets[j].ID().String() })
				encodedTier := tierDTO{Targets: make([]targetDTO, 0, len(targets))}
				for _, target := range targets {
					encodedTarget, err := encodeTarget(target)
					if err != nil {
						return nil, err
					}
					encodedTier.Targets = append(encodedTier.Targets, encodedTarget)
				}
				encodedRoute.Tiers = append(encodedRoute.Tiers, encodedTier)
			}
			encodedWorkspace.Routes = append(encodedWorkspace.Routes, encodedRoute)
		}
		dto.Workspaces[workspace.Slug().String()] = encodedWorkspace
	}
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(dto); err != nil {
		return nil, fmt.Errorf("encode routing config: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close routing encoder: %w", err)
	}
	return buffer.Bytes(), nil
}

func encodeTarget(target routing.Target) (targetDTO, error) {
	version := uint64(target.Version())
	return targetDTO{ID: target.ID().String(), Version: &version, Model: target.Model().String(), Protocol: target.Protocol().String(), ConnectionID: target.ConnectionID().String()}, nil
}

// unknownFieldTypeSuffix strips yaml.v3's internal Go type name (e.g.
// "configstore.documentDTO") from "field X not found in type T" messages.
var unknownFieldTypeSuffix = regexp.MustCompile(`not found in type \S+`)

// readableDecodeError rewrites yaml.v3's strict-mode type errors so they name
// the unrecognized config field instead of an internal struct type; other
// decode errors pass through unchanged.
func readableDecodeError(err error) error {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return err
	}
	cleaned := make([]string, 0, len(typeErr.Errors))
	for _, msg := range typeErr.Errors {
		cleaned = append(cleaned, unknownFieldTypeSuffix.ReplaceAllString(msg, "is not a recognized config field"))
	}
	return fmt.Errorf("config has unrecognized field(s) (the file may be from an older swobu schema):\n  %s", strings.Join(cleaned, "\n  "))
}

func prefixPath(err error, prefix string) error {
	var invariant *routing.InvariantError
	if ok := asInvariant(err, &invariant); ok {
		invariantCopy := *invariant
		if !strings.HasPrefix(invariantCopy.Path, prefix) {
			invariantCopy.Path = prefix + "." + invariantCopy.Path
		}
		return &invariantCopy
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func asInvariant(err error, target **routing.InvariantError) bool {
	for err != nil {
		if value, ok := err.(*routing.InvariantError); ok {
			*target = value
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}
