package clientconnect

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Target is the canonical workspace address consumed by every
// supported client adapter. Workspace identity never includes /v1.
type Target struct {
	workspaceSlug string
	workspaceURL  string
	modelID       string
	local         bool
	principal     string
	bearer        string
}

// NewTarget accepts the canonical root or the tolerated /v1 input spelling and
// always returns the one unversioned workspace URL.
func NewTarget(workspaceSlug, workspaceURL string) (Target, error) {
	parsed, err := normalizeWorkspaceRoot(workspaceSlug, workspaceURL)
	if err != nil {
		return Target{}, err
	}
	base := strings.TrimRight(parsed.String(), "/")
	host := parsed.Hostname()
	return Target{
		workspaceSlug: workspaceSlug,
		workspaceURL:  base,
		local:         host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback(),
	}, nil
}

// WorkspaceSlug returns the validated workspace identity.
func (t Target) WorkspaceSlug() string { return t.workspaceSlug }

// WorkspaceURL returns the canonical unversioned workspace URL.
func (t Target) WorkspaceURL() string { return t.workspaceURL }

// ModelID is the actual client-requested ID that selects the first matching route.
func (t Target) ModelID() string { return t.modelID }

// WithModelID selects one concrete model ID for automatic client configuration.
func (t Target) WithModelID(modelID string) Target { t.modelID = modelID; return t }

// IsLocal reports whether the validated URL addresses a loopback host.
func (t Target) IsLocal() bool { return t.local }

// ValidateLocal rejects non-canonical or non-loopback targets before Connect
// may inspect or mutate a client configuration.
func (t Target) ValidateLocal() error { return t.validateLocal() }

func (t Target) validateLocal() error {
	canonicalURL, err := normalizeWorkspaceRoot(t.workspaceSlug, t.workspaceURL)
	if err != nil {
		return fmt.Errorf("automatic client wiring requires a canonical loopback Swobu workspace")
	}
	host := canonicalURL.Hostname()
	canonical := Target{workspaceSlug: t.workspaceSlug, workspaceURL: strings.TrimRight(canonicalURL.String(), "/"), local: host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()}
	canonical.principal, canonical.bearer, canonical.modelID = t.principal, t.bearer, t.modelID
	if canonical != t {
		return fmt.Errorf("automatic client wiring requires a canonical loopback Swobu workspace")
	}
	if !canonical.local {
		return fmt.Errorf("automatic client wiring requires a loopback Swobu workspace")
	}
	return nil
}

// normalizeWorkspaceRoot makes the ambiguous client-base input contract
// explicit: callers may supply a Workspace root or its /v1 suffix, never an operation URL.
func normalizeWorkspaceRoot(workspaceSlug, workspaceURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimRight(workspaceURL, "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("workspace endpoint is not a complete URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("workspace endpoint must use http or https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("workspace endpoint must not contain credentials, query, or fragment")
	}
	wantPath := "/workspaces/" + workspaceSlug
	path := parsed.EscapedPath()
	if path != wantPath && path != wantPath+"/v1" {
		return nil, fmt.Errorf("workspace endpoint path must be %s with optional /v1", wantPath)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/v1")
	parsed.RawPath = strings.TrimSuffix(parsed.RawPath, "/v1")
	return parsed, nil
}
