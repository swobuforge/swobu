package httpapi

import (
	"context"
	_ "embed"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/sharestate"
)

// ShareHandler serves the Owner-terminated public Share HTTP boundary.
type ShareHandler struct {
	shareStore  *sharestate.Store
	configStore *configstore.Store
	ingress     exchange.RequestIngress
}

// NewShareHandler constructs the public Share adapter around existing Share
// state and ingress.
func NewShareHandler(shareStore *sharestate.Store, configStore *configstore.Store, ingress exchange.RequestIngress) ShareHandler {
	return ShareHandler{shareStore: shareStore, configStore: configStore, ingress: ingress}
}

// ServeHTTP applies public Share HTTP policy before delegating authenticated
// requests.
func (h ShareHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setShareCORSHeaders(w)
	// Preflight carries no Share authority and must terminate before any
	// dependency access.
	if isCORSPreflight(r) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/" {
		serveShareInvite(w)
		return
	}
	bearer := shareBearer(r)
	grant, err := h.shareStore.Authenticate(bearer)
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="swobu-share"`)
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	workspace, err := h.configStore.GetWorkspace(r.Context(), grant.Workspace)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	projected := workspace
	workspaceScope := grant.Route.String() == ""
	if !workspaceScope {
		route, ok := workspace.Route(grant.Route)
		if !ok {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		projected, err = routing.NewWorkspace(workspace.Slug(), route.Name(), []routing.Route{route})
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}
	bound := workspaceBoundIngress{workspace: projected, ingress: h.ingress, workspaceScope: workspaceScope}
	request := r.Clone(r.Context())
	request = request.WithContext(context.WithValue(request.Context(), admissionContextKey{}, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) { return accessidentity.Share, nil })))
	consumeShareBearer(request, bearer)
	request.URL.Path = "/c/" + projected.Slug().String() + shareProtocolPath(r.URL.Path)
	NewHandler(bound).ServeHTTP(w, request)
}

func consumeShareBearer(r *http.Request, bearer string) {
	for _, name := range []string{"Authorization", "x-api-key"} {
		values := r.Header.Values(name)
		r.Header.Del(name)
		for _, value := range values {
			candidate := strings.TrimSpace(value)
			if strings.HasPrefix(candidate, "Bearer ") {
				candidate = strings.TrimSpace(strings.TrimPrefix(candidate, "Bearer "))
			}
			if candidate != bearer {
				r.Header.Add(name, value)
			}
		}
	}
}

type workspaceBoundIngress struct {
	workspace      routing.Workspace
	ingress        exchange.RequestIngress
	workspaceScope bool
}

func (b workspaceBoundIngress) HandleRequest(ctx context.Context, in exchange.RequestInput) (exchange.RequestOutput, error) {
	return b.ingress.HandleRequestWithWorkspace(ctx, b.workspace, in)
}

func (b workspaceBoundIngress) ListModels(ctx context.Context, in exchange.ListModelsInput) (exchange.ListModelsOutput, error) {
	if b.workspaceScope {
		return exchange.ListModelsWithWorkspace(b.workspace), nil
	}
	return exchange.ListModelsOutput{DefaultModelID: routing.PublicDefaultRouteID}, nil
}

func setShareCORSHeaders(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Access-Control-Allow-Origin", "*")
	header.Set("Access-Control-Allow-Methods", "GET, POST")
	header.Set("Access-Control-Allow-Headers", "Authorization, x-api-key, Content-Type, Anthropic-Version")
}

func isCORSPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions &&
		r.Header.Get("Origin") != "" &&
		r.Header.Get("Access-Control-Request-Method") != ""
}

func shareBearer(r *http.Request) string {
	if authorization := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(authorization, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	}
	return strings.TrimSpace(r.Header.Get("x-api-key"))
}

func shareProtocolPath(path string) string {
	if strings.HasPrefix(path, "/v1/") {
		return strings.TrimPrefix(path, "/v1")
	}
	return path
}

func serveShareInvite(w http.ResponseWriter) {
	setShareSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(shareInvitePage)
}

func setShareSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; frame-ancestors 'none'")
}

var (
	//go:embed share_invite/index.html
	shareInviteHTML string
	//go:embed share_invite/styles.css
	shareInviteCSS string
	//go:embed share_invite/vendor/qrcode.min.js
	shareInviteQRCode string
	//go:embed share_invite/app.js
	shareInviteApp string

	// The response remains one document so the bearer cannot leak through asset
	// requests. Replacement markers are private build seams, never user input.
	shareInvitePage = []byte(strings.NewReplacer(
		"/* SWOBU_STYLES */", shareInviteCSS,
		"/* SWOBU_QRCODE */", shareInviteQRCode,
		"/* SWOBU_APP */", shareInviteApp,
	).Replace(shareInviteHTML))
)
