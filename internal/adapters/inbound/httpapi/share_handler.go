package httpapi

import (
	"context"
	_ "embed"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/sharestate"
)

type ShareHandler struct {
	shareStore  *sharestate.Store
	configStore *configstore.Store
	ingress     exchange.RequestIngress
	traffic     observation.TrafficEventSink
}

func NewShareHandler(shareStore *sharestate.Store, configStore *configstore.Store, ingress exchange.RequestIngress, traffic observation.TrafficEventSink) ShareHandler {
	return ShareHandler{shareStore: shareStore, configStore: configStore, ingress: ingress, traffic: traffic}
}

func (h ShareHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	request.URL.Path = "/c/" + projected.Slug().String() + shareProtocolPath(r.URL.Path)
	NewHandler(bound, h.traffic).ServeHTTP(w, request)
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
