package httpapi

import (
	"context"
	"html/template"
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
	route, ok := workspace.Route(grant.Route)
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	projected, err := routing.NewWorkspace(workspace.Slug(), route.Name(), []routing.Route{route})
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	bound := workspaceBoundIngress{workspace: projected, ingress: h.ingress}
	request := r.Clone(r.Context())
	request.URL.Path = "/c/" + projected.Slug().String() + shareProtocolPath(r.URL.Path)
	NewHandler(bound, h.traffic).ServeHTTP(w, request)
}

type workspaceBoundIngress struct {
	workspace routing.Workspace
	ingress   exchange.RequestIngress
}

func (b workspaceBoundIngress) HandleRequest(ctx context.Context, in exchange.RequestInput) (exchange.RequestOutput, error) {
	return b.ingress.HandleRequestWithWorkspace(ctx, b.workspace, in)
}

func (b workspaceBoundIngress) ListModels(context.Context, exchange.ListModelsInput) (exchange.ListModelsOutput, error) {
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
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = shareInviteTemplate.Execute(w, nil)
}

var shareInviteTemplate = template.Must(template.New("share-invite").Parse(`<!doctype html><meta charset="utf-8"><meta name="referrer" content="no-referrer"><title>Swobu Shared Route</title><style>body{font:16px system-ui;max-width:48rem;margin:4rem auto;padding:0 1rem}code{word-break:break-all}</style><h1>Swobu Shared Route</h1><p>Use these values in any OpenAI- or Anthropic-compatible client.</p><dl><dt>OpenAI Base URL</dt><dd><code id="openai"></code></dd><dt>Anthropic Base URL</dt><dd><code id="anthropic"></code></dd><dt>API key</dt><dd><code id="key"></code></dd></dl><script>const key=location.hash.slice(1);history.replaceState(null,'',location.pathname);const base=location.origin;document.getElementById('openai').textContent=base+'/v1';document.getElementById('anthropic').textContent=base;document.getElementById('key').textContent=key;if(!key.startsWith('swsh_'))document.getElementById('key').textContent='Open the complete invite URL to reveal the API key.';</script>`))
