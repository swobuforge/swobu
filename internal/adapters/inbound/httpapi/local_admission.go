package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/publications"
	"github.com/swobuforge/swobu/internal/routing"
)

var errLocalAdmissionForbidden = errors.New("local daemon access is forbidden")

type admittedAuthority func(routing.WorkspaceSlug) (accessidentity.ID, error)
type admittedServiceAuthority func(routing.WorkspaceSlug, routing.ServiceKind) (accessidentity.ID, error)
type admissionContextKey struct{}
type serviceAdmissionContextKey struct{}
type serviceRateAdmissionContextKey struct{}
type ingressServiceContextKey struct{}
type admittedServiceRate func(routing.WorkspaceSlug, routing.ServiceKind) (time.Duration, error)

type workspaceLimitKey struct{}

// WorkspaceLimitAdmission is the common daemon-owned operation limiter.
// It never authenticates a Principal or changes Workspace permissions.
type WorkspaceLimitAdmission func(routing.WorkspaceSlug, routing.ServiceKind) (func(), time.Duration, error)

func withWorkspaceLimit(ctx context.Context, limit WorkspaceLimitAdmission) context.Context {
	if limit == nil {
		return ctx
	}
	return context.WithValue(ctx, workspaceLimitKey{}, limit)
}

func AdmitWorkspaceOperationalOperation(ctx context.Context, workspace routing.WorkspaceSlug, service routing.ServiceKind) (func(), time.Duration, error) {
	if limit, ok := ctx.Value(workspaceLimitKey{}).(WorkspaceLimitAdmission); ok && limit != nil {
		return limit(workspace, service)
	}
	return func() {}, 0, nil
}

// A client request has exactly one directly addressable Service. Unknown
// Workspace data paths must not acquire a managed credential's authority.
func requestedWorkspaceService(path string) routing.ServiceKind {
	if !strings.HasPrefix(path, "/workspaces/") {
		return ""
	}
	_, operation, err := splitProtocolPath(path, "/workspaces/")
	if err != nil {
		return ""
	}
	switch {
	case operation == "/mcp":
		return routing.ServiceMCP
	case operation == "/search":
		return routing.ServiceSearch
	case operation == "/v1" || strings.HasPrefix(operation, "/v1/") || strings.HasPrefix(operation, "/v1beta/") || strings.HasPrefix(operation, "/v1alpha/") || operation == "/models" || operation == "/responses" || operation == "/messages" || strings.HasPrefix(operation, "/chat/") || strings.HasPrefix(operation, "/completions") || strings.HasPrefix(operation, "/embeddings"):
		return routing.ServiceModels
	default:
		return ""
	}
}

// NewLocalAdmissionHandler protects the complete local daemon surface and
// resolves managed Access credentials before protocol decoding. Public relay
// traffic uses a separate authenticated Workspace-only boundary. The daemon
// supplies its shared limiter explicitly; nil is only for isolated adapters
// whose caller owns operational admission.
func NewLocalAdmissionHandler(next http.Handler, state *accessstate.Store, limit WorkspaceLimitAdmission) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := ValidateLocalAdmission(r); err != nil {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		controlPath := r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/")
		// Local control has no bearer credential audience. A supplied credential
		// must not become native authority because its prefix was unrecognized.
		// Data protocols retain their independently owned credential parsing.
		if controlPath && len(accessCredentialSlots(r)) != 0 && len(recognizedAccessCredentials(r)) == 0 {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		// A presented data credential must authenticate in the Workspace
		// audience. Unknown Node-shaped/provider keys cannot select the native
		// installation identity, even when the peer is loopback.
		dataPath := strings.HasPrefix(r.URL.Path, "/workspaces/")
		if dataPath && len(accessCredentialSlots(r)) != 0 && len(recognizedAccessCredentials(r)) == 0 {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		// Run before credential resolution and ServeMux path cleaning. Otherwise
		// malformed Workspace paths can authenticate or redirect into a different
		// namespace before the protocol handler gets to reject their ambiguity.
		if (strings.HasPrefix(r.URL.Path, "/workspaces/") && ambiguousWorkspacePath(r) && !modelRouteDocumentPath(r)) || (strings.HasPrefix(r.URL.Path, "/api/v1/auth-sessions/") && (ambiguousWorkspacePath(r) || r.URL.EscapedPath() != r.URL.Path)) {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		// Save the recognized candidate before the admitted resolver erases it
		// from the request. No bearer is stored in the resulting context.
		candidates := recognizedAccessCredentials(r)
		authority, status, err := resolveRequestAuthority(r, state)
		if err != nil {
			http.Error(w, http.StatusText(status), status)
			return
		}
		admitted := r.WithContext(withWorkspaceLimit(contextWithServiceAuthority(r, authority, state, candidates), limit))
		if !admitMeteredHTTP(w, admitted) {
			return
		}
		release, allowed := admitOperationalHTTP(w, admitted)
		if !allowed {
			return
		}
		defer release()
		// A valid Workspace bearer authenticates data use, not Node management.
		// Protect canonical and Node Control before any handler can
		// disclose metadata or issue credentials through an unguarded leaf.
		control := r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/")
		if control && !IsLocalOperatorRequest(admitted) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, admitted)
	})
}

// modelRouteDocumentPath permits escaping only the final pattern segment of a
// local Operator document. It never relaxes data-plane or public path admission;
// browser session admission still runs before the document handler.
func modelRouteDocumentPath(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
	if len(parts) != 5 || parts[0] != "workspaces" || parts[2] != "services" || parts[3] != "models" {
		return false
	}
	if _, err := routing.ParseWorkspaceSlug(parts[1]); err != nil {
		return false
	}
	pattern, err := url.PathUnescape(parts[4])
	if err != nil {
		return false
	}
	_, err = routing.ParseRouteName(pattern)
	return err == nil
}

// NewPublicWorkspaceHandler constructs a data-plane-only public ingress. It
// admits only exact Workspace paths covered by Workspace publication and never wraps
// or filters the local Node Control mux.
func NewPublicWorkspaceHandler(next http.Handler, state *accessstate.Store, publicationStore *publications.Store, limit WorkspaceLimitAdmission) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setPublicWorkspaceCORSHeaders(w)
		if publicWorkspaceMCPPath(r.URL.Path) {
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, x-api-key, Content-Type, Anthropic-Version, Mcp-Method, Mcp-Name, Mcp-Protocol-Version")
		}
		w.Header().Set("Cache-Control", "no-store")
		if publicWorkspaceCORSPreflight(r) {
			if !publicWorkspacePreflightAllowed(publicationStore, r) {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if publicationStore == nil || state == nil || !strings.HasPrefix(r.URL.Path, "/workspaces/") || ambiguousWorkspacePath(r) {
			http.NotFound(w, r)
			return
		}
		slug, _, err := splitProtocolPath(r.URL.Path, "/workspaces/")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		workspace, err := routing.ParseWorkspaceSlug(slug)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if !publicWorkspaceOperation(r) && !publicWorkspaceMCPRequest(r) && !publicWorkspaceSearchRequest(r) {
			http.NotFound(w, r)
			return
		}
		if !publicationStore.Has(workspace) {
			http.NotFound(w, r)
			return
		}
		candidates := recognizedAccessCredentials(r)
		admitted, status, err := resolvePublicWorkspaceAuthority(r, state, publicationStore, workspace)
		if err != nil {
			http.Error(w, http.StatusText(status), status)
			return
		}
		admittedRequest := r.WithContext(withWorkspaceLimit(contextWithServiceAuthority(r, admitted, state, candidates), limit))
		// The relay does not use ServeMux path templates; give downstream
		// Workspace handlers the same canonical slug as the admission gate.
		admittedRequest.SetPathValue("workspace", workspace.String())
		if !admitMeteredHTTP(w, admittedRequest) {
			return
		}
		release, allowed := admitOperationalHTTP(w, admittedRequest)
		if !allowed {
			return
		}
		defer release()
		next.ServeHTTP(w, admittedRequest)
	})
}

func setPublicWorkspaceCORSHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, POST")
	h.Set("Access-Control-Allow-Headers", "Authorization, x-api-key, Content-Type, Anthropic-Version")
}

func publicWorkspaceCORSPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions &&
		r.Header.Get("Origin") != "" &&
		r.Header.Get("Access-Control-Request-Method") != ""
}

func publicWorkspacePreflightAllowed(store *publications.Store, request *http.Request) bool {
	if store == nil || request == nil || !strings.HasPrefix(request.URL.Path, "/workspaces/") || ambiguousWorkspacePath(request) {
		return false
	}
	slug, _, err := splitProtocolPath(request.URL.Path, "/workspaces/")
	if err != nil {
		return false
	}
	workspace, err := routing.ParseWorkspaceSlug(slug)
	if err != nil || !store.Has(workspace) {
		return false
	}
	method := strings.ToUpper(strings.TrimSpace(request.Header.Get("Access-Control-Request-Method")))
	if method != http.MethodGet && method != http.MethodPost {
		return false
	}
	probe := request.Clone(request.Context())
	probe.Method = method
	return publicWorkspaceOperation(probe) || publicWorkspaceMCPRequest(probe) || publicWorkspaceSearchRequest(probe)
}

func publicWorkspaceMCPRequest(r *http.Request) bool {
	_, operationPath, err := splitProtocolPath(r.URL.Path, "/workspaces/")
	return err == nil && operationPath == "/mcp" && r.Method == http.MethodPost
}

// Direct Search is a separate Workspace service, never a Model API alias.
// The public listener accepts its exact GET/POST endpoint only when explicitly
// published. Search admission still checks the credential's Search grant.
func publicWorkspaceSearchRequest(r *http.Request) bool {
	_, operationPath, err := splitProtocolPath(r.URL.Path, "/workspaces/")
	return err == nil && operationPath == "/search" && (r.Method == http.MethodPost || r.Method == http.MethodGet)
}

func publicWorkspaceMCPPath(path string) bool {
	_, operationPath, err := splitProtocolPath(path, "/workspaces/")
	return err == nil && operationPath == "/mcp"
}

// publicWorkspaceOperation accepts only LLM protocol paths. MCP has its own
// exact POST operation check so LLM path parsing cannot broaden its transport.
func publicWorkspaceOperation(r *http.Request) bool {
	_, operationPath, err := splitProtocolPath(r.URL.Path, "/workspaces/")
	if err != nil || operationPath == "" || strings.ContainsAny(operationPath, "?#") {
		return false
	}
	if operationPath == "/v1/models" || operationPath == "/models" {
		return r.Method == http.MethodGet
	}
	if strings.HasPrefix(operationPath, "/v1/") {
		operationPath = strings.TrimPrefix(operationPath, "/v1")
	}
	queryPath := operationPath
	if r.Method == http.MethodGet && websocketUpgrade(r) {
		path, err := canonical.NormalizePath(operationPath)
		return err == nil && path == canonical.NormalizedPathResponses
	}
	if r.URL.RawQuery != "" {
		queryPath += "?" + r.URL.RawQuery
	}
	_, err = canonical.ParseClientOperation(r.Method, queryPath, strings.TrimSpace(r.Header.Get("anthropic-version")) != "", false)
	return err == nil
}

func resolvePublicWorkspaceAuthority(r *http.Request, state *accessstate.Store, publicationStore *publications.Store, workspace routing.WorkspaceSlug) (admittedAuthority, int, error) {
	candidates := recognizedAccessCredentials(r)
	if len(candidates) == 0 {
		return nil, http.StatusUnauthorized, errors.New("Workspace credential required")
	}
	if len(candidates) != 1 {
		return nil, http.StatusUnauthorized, errors.New("ambiguous Workspace credentials")
	}
	resolution, err := state.Authenticate(candidates[0])
	if err != nil {
		return nil, http.StatusUnauthorized, err
	}
	consumeAccessCredential(r, candidates[0])
	service := requestedWorkspaceService(r.URL.Path)
	if service == "" {
		return nil, http.StatusForbidden, errWorkspaceForbidden
	}
	if _, err := state.ResolveCredentialForService(resolution.CredentialID, service); err != nil {
		return nil, http.StatusForbidden, err
	}
	return func(candidate routing.WorkspaceSlug) (accessidentity.ID, error) {
		current, err := state.ResolveCredentialForService(resolution.CredentialID, service)
		if err != nil {
			return accessidentity.ID{}, err
		}
		if candidate != workspace || !current.Audience().AllowsWorkspace(candidate.String()) || !publicationStore.Has(candidate) {
			return accessidentity.ID{}, errWorkspaceForbidden
		}
		return current.AccessID, nil
	}, 0, nil
}

func contextWithAuthority(r *http.Request, authority admittedAuthority) context.Context {
	return context.WithValue(r.Context(), admissionContextKey{}, authority)
}

// contextWithServiceAuthority retains an opaque credential ID, never a bearer.
// The normal admitted authority remains the first check for Workspace,
// publication and revocation. A cross-Service composition must then revalidate
// the additional Service against the very same credential at effect time.
func contextWithServiceAuthority(r *http.Request, authority admittedAuthority, state *accessstate.Store, candidates []string) context.Context {
	ctx := context.WithValue(contextWithAuthority(r, authority), ingressServiceContextKey{}, requestedWorkspaceService(r.URL.Path))
	if state == nil || len(candidates) != 1 {
		return ctx
	}
	resolution, err := state.Authenticate(candidates[0])
	if err != nil {
		// The first admission may have been revoked in between lookups; the
		// admitted authority closure will deny it at effect time as well.
		return ctx
	}
	check := admittedServiceAuthority(func(workspace routing.WorkspaceSlug, service routing.ServiceKind) (accessidentity.ID, error) {
		identity, err := authority(workspace)
		if err != nil {
			return accessidentity.ID{}, err
		}
		current, err := state.ResolveCredentialForService(resolution.CredentialID, service)
		if err != nil {
			return accessidentity.ID{}, err
		}
		if !current.Audience().AllowsWorkspace(workspace.String()) || current.AccessID != identity {
			return accessidentity.ID{}, errWorkspaceForbidden
		}
		return identity, nil
	})
	ctx = context.WithValue(ctx, serviceAdmissionContextKey{}, check)
	charge := admittedServiceRate(func(workspace routing.WorkspaceSlug, service routing.ServiceKind) (time.Duration, error) {
		return state.AdmitCredentialOperation(resolution.CredentialID, workspace, service)
	})
	return context.WithValue(ctx, serviceRateAdmissionContextKey{}, charge)
}

// AdmitWorkspaceServiceOperation meters a real downstream operation only after
// rechecking current credential/grant authority. One Principal's credentials
// share the same in-process rate bucket; native host and native host authority
// are not managed Principal grants and have no such policy.
func AdmitWorkspaceServiceOperation(ctx context.Context, workspace routing.WorkspaceSlug, service routing.ServiceKind) (time.Duration, error) {
	if _, err := WorkspaceServiceAuthorityFromContext(ctx, workspace, service); err != nil {
		return 0, err
	}
	if charge, ok := ctx.Value(serviceRateAdmissionContextKey{}).(admittedServiceRate); ok && charge != nil {
		return charge(workspace, service)
	}
	return 0, nil
}

// A Models request can invoke MCP tools. Principal service admission checks
// apply separately at that effect boundary. Direct MCP RPCs are already
// admitted to the Service; Workspace operational budget is charged later,
// only when the handler actually invokes tools/call.
func AdmitComposedServiceOperation(ctx context.Context, workspace routing.WorkspaceSlug, service routing.ServiceKind) (time.Duration, error) {
	if from, _ := ctx.Value(ingressServiceContextKey{}).(routing.ServiceKind); from == service {
		if _, err := WorkspaceServiceAuthorityFromContext(ctx, workspace, service); err != nil {
			return 0, err
		}
		return 0, nil
	}
	return AdmitWorkspaceServiceOperation(ctx, workspace, service)
}

func meteredWorkspace(r *http.Request) (routing.WorkspaceSlug, error) {
	raw, _, err := splitProtocolPath(r.URL.Path, "/workspaces/")
	if err != nil {
		return routing.WorkspaceSlug{}, err
	}
	return routing.ParseWorkspaceSlug(raw)
}

// HTTP POST and direct Search GET each count one downstream RPC regardless of upstream retries or streaming
// duration. WebSocket upgrades use GET, which is not charged here; each valid
// response.create message is charged separately in the WebSocket handler.
// In particular, untrusted Upgrade headers on a POST must never skip metering.
func admitMeteredHTTP(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost && !publicWorkspaceSearchRequest(r) {
		return true
	}
	service := requestedWorkspaceService(r.URL.Path)
	if service == "" {
		return true
	}
	workspace, err := meteredWorkspace(r)
	if err != nil {
		http.NotFound(w, r)
		return false
	}
	// Preserve the data handler's audience concealment before quota admission.
	// Models reports forbidden; MCP and Search conceal the absent audience.
	if _, err := WorkspaceAuthorityFromContext(r.Context(), workspace); err != nil {
		if service == routing.ServiceModels {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		} else {
			http.NotFound(w, r)
		}
		return false
	}
	retry, err := AdmitWorkspaceServiceOperation(r.Context(), workspace, service)
	if err == nil {
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	if errors.Is(err, accessstate.ErrRequestRateLimited) {
		seconds := int((retry + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		http.Error(w, "Principal Service request limit exceeded", http.StatusTooManyRequests)
	} else {
		http.Error(w, "Workspace credential is not authorized", http.StatusForbidden)
	}
	return false
}

// WorkspaceServiceAuthorityFromContext requires a fresh Service-specific
// authorization whenever one Workspace Service invokes another. Native local
// operators are independently admitted; a
// managed Workspace credential can never fall back to that broader authority.
func WorkspaceServiceAuthorityFromContext(ctx context.Context, workspace routing.WorkspaceSlug, service routing.ServiceKind) (accessidentity.ID, error) {
	identity, err := WorkspaceAuthorityFromContext(ctx, workspace)
	if err != nil {
		return accessidentity.ID{}, err
	}
	if _, err := routing.ParseServiceKind(string(service)); err != nil {
		return accessidentity.ID{}, err
	}
	if check, ok := ctx.Value(serviceAdmissionContextKey{}).(admittedServiceAuthority); ok && check != nil {
		validated, err := check(workspace, service)
		if err != nil || validated != identity {
			return accessidentity.ID{}, errWorkspaceForbidden
		}
		return identity, nil
	}
	if identity == accessidentity.Local {
		return identity, nil
	}
	return accessidentity.ID{}, errWorkspaceForbidden
}

func authorityFromRequest(r *http.Request, workspace routing.WorkspaceSlug) (accessidentity.ID, error) {
	return WorkspaceAuthorityFromContext(r.Context(), workspace)
}

// WorkspaceAuthorityFromContext rechecks the admitted credential's current
// authority immediately before a downstream effect. The private closure owns
// credential identity; callers neither retain nor reconstruct bearer secrets.
func WorkspaceAuthorityFromContext(ctx context.Context, workspace routing.WorkspaceSlug) (accessidentity.ID, error) {
	if err := ctx.Err(); err != nil {
		return accessidentity.ID{}, err
	}
	authority, ok := ctx.Value(admissionContextKey{}).(admittedAuthority)
	if !ok || authority == nil {
		return accessidentity.ID{}, errors.New("request has no admitted Access authority")
	}
	identity, err := authority(workspace)
	if err != nil {
		return accessidentity.ID{}, err
	}
	// A successful lookup without an identity is not an anonymous grant. The
	// private admission seam must resolve attribution before authorizing effects.
	if !identity.Valid() {
		return accessidentity.ID{}, errors.New("admission resolved no Access identity")
	}
	if err := ctx.Err(); err != nil {
		return accessidentity.ID{}, err
	}
	return identity, nil
}

// AuthorizeWorkspaceRequest exposes the already-admitted local authority check
// to separately constructed Workspace protocol handlers such as MCP.
func AuthorizeWorkspaceRequest(r *http.Request, rawWorkspace string) bool {
	workspace, err := routing.ParseWorkspaceSlug(rawWorkspace)
	if err != nil {
		return false
	}
	_, err = authorityFromRequest(r, workspace)
	return err == nil
}

// IsLocalOperatorRequest reports whether admission resolved the built-in local
// operator context. Managed Workspace credentials intentionally return false.
func IsLocalOperatorRequest(r *http.Request) bool {
	// Workspace audience lookup success cannot manufacture Node authority. The
	// built-in local identity must resolve from live, private ingress context.
	identity, err := WorkspaceAuthorityFromContext(r.Context(), routing.WorkspaceSlug{})
	return err == nil && identity == accessidentity.Local
}

func resolveRequestAuthority(r *http.Request, state *accessstate.Store) (admittedAuthority, int, error) {
	candidates := recognizedAccessCredentials(r)
	if len(candidates) == 0 {
		return func(routing.WorkspaceSlug) (accessidentity.ID, error) { return accessidentity.Local, nil }, 0, nil
	}
	if len(candidates) > 1 {
		return nil, http.StatusUnauthorized, errors.New("ambiguous Access credentials")
	}
	resolution, err := state.Authenticate(candidates[0])
	if err != nil {
		return nil, http.StatusUnauthorized, err
	}
	consumeAccessCredential(r, candidates[0])
	credentialID := resolution.CredentialID
	service := requestedWorkspaceService(r.URL.Path)
	if (strings.HasPrefix(r.URL.Path, "/workspaces/")) && service == "" {
		return nil, http.StatusForbidden, errWorkspaceForbidden
	}
	if service != "" {
		if _, err := state.ResolveCredentialForService(credentialID, service); err != nil {
			return nil, http.StatusForbidden, err
		}
	}
	return func(workspace routing.WorkspaceSlug) (accessidentity.ID, error) {
		current, err := state.ResolveCredential(credentialID)
		if service != "" {
			current, err = state.ResolveCredentialForService(credentialID, service)
		}
		if err != nil {
			return accessidentity.ID{}, err
		}
		if !current.Audience().AllowsWorkspace(workspace.String()) {
			return accessidentity.ID{}, errWorkspaceForbidden
		}
		return current.AccessID, nil
	}, 0, nil
}

var errWorkspaceForbidden = errors.New("credential does not grant the requested Workspace")

func recognizedAccessCredentials(r *http.Request) []string {
	seen := map[string]struct{}{}
	for _, slot := range accessCredentialSlots(r) {
		if strings.HasPrefix(slot.token, "swak_") {
			seen[slot.token] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	return out
}

type accessCredentialLocation uint8

const (
	accessCredentialAuthorization accessCredentialLocation = iota + 1
	accessCredentialHeader
	accessCredentialQuery
)

type accessCredentialSlot struct {
	location accessCredentialLocation
	name     string
	raw      string
	token    string
}

// accessCredentialSlots is the sole parser for managed credential locations.
// Detection and erasure consume the same normalized occurrences, preventing a
// recognized secret from surviving under subtly different parsing semantics.
func accessCredentialSlots(r *http.Request) []accessCredentialSlot {
	var slots []accessCredentialSlot
	for _, raw := range r.Header.Values("Authorization") {
		token := ""
		fields := strings.Fields(raw)
		if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
			token = normalizeAccessCredential(fields[1])
		}
		slots = append(slots, accessCredentialSlot{location: accessCredentialAuthorization, name: "Authorization", raw: raw, token: token})
	}
	for _, name := range []string{"x-api-key", "X-Goog-Api-Key"} {
		for _, raw := range r.Header.Values(name) {
			slots = append(slots, accessCredentialSlot{location: accessCredentialHeader, name: name, raw: raw, token: normalizeAccessCredential(raw)})
		}
	}
	for _, raw := range r.URL.Query()["key"] {
		slots = append(slots, accessCredentialSlot{location: accessCredentialQuery, name: "key", raw: raw, token: normalizeAccessCredential(raw)})
	}
	return slots
}

func normalizeAccessCredential(raw string) string { return strings.TrimSpace(raw) }

func consumeAccessCredential(r *http.Request, token string) {
	slots := accessCredentialSlots(r)
	for _, name := range []string{"Authorization", "x-api-key", "X-Goog-Api-Key"} {
		r.Header.Del(name)
	}
	query := r.URL.Query()
	query.Del("key")
	for _, slot := range slots {
		if slot.token == token {
			continue
		}
		switch slot.location {
		case accessCredentialAuthorization, accessCredentialHeader:
			r.Header.Add(slot.name, slot.raw)
		case accessCredentialQuery:
			query.Add("key", slot.raw)
		}
	}
	r.URL.RawQuery = query.Encode()
	r.RequestURI = r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		r.RequestURI += "?" + r.URL.RawQuery
	}
}

func ValidateLocalAdmission(request *http.Request) error {
	peerHost, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err != nil {
		return errLocalAdmissionForbidden
	}
	peer, err := netip.ParseAddr(peerHost)
	if err != nil || !peer.IsLoopback() {
		return errLocalAdmissionForbidden
	}
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	requestAuthority, ok := normalizedLoopbackAuthority(request.Host, scheme)
	if !ok {
		return errLocalAdmissionForbidden
	}
	origins := request.Header.Values("Origin")
	if len(origins) == 0 {
		return nil
	}
	if len(origins) != 1 {
		return errLocalAdmissionForbidden
	}
	rawOrigin := strings.TrimSpace(origins[0])
	if rawOrigin == "" || strings.EqualFold(rawOrigin, "null") {
		return errLocalAdmissionForbidden
	}
	origin, err := url.Parse(rawOrigin)
	if err != nil || !strings.EqualFold(origin.Scheme, scheme) || origin.Host == "" || origin.User != nil || origin.Opaque != "" || origin.Path != "" || origin.RawPath != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" {
		return errLocalAdmissionForbidden
	}
	originAuthority, ok := normalizedLoopbackAuthority(origin.Host, scheme)
	if !ok || originAuthority != requestAuthority {
		return errLocalAdmissionForbidden
	}
	return nil
}

func normalizedLoopbackAuthority(authority, scheme string) (string, bool) {
	raw := strings.TrimSpace(authority)
	if raw == "" || strings.Contains(raw, "%") {
		return "", false
	}
	parsed, err := url.Parse("//" + raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || strings.HasSuffix(parsed.Host, ":") {
		return "", false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		host = "localhost"
	} else {
		address, e := netip.ParseAddr(host)
		if e != nil || !address.IsLoopback() {
			return "", false
		}
		host = address.String()
	}
	port := parsed.Port()
	if port == "" {
		if strings.EqualFold(scheme, "http") {
			port = "80"
		} else if strings.EqualFold(scheme, "https") {
			port = "443"
		} else {
			return "", false
		}
	}
	number, e := strconv.Atoi(port)
	if e != nil || number < 1 || number > 65535 {
		return "", false
	}
	return net.JoinHostPort(host, strconv.Itoa(number)), true
}
