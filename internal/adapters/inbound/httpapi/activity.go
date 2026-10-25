package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	executionevidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/routing"
)

type activityReadFunc func(*http.Request, executionevidence.ProjectionScope, ledger.ActivityPageQuery) (executionevidence.ActivityProjection, error)

type ActivityHandler struct{ read activityReadFunc }

func NewActivityHandler(read activityReadFunc) ActivityHandler { return ActivityHandler{read: read} }

func (h ActivityHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeOperatorError(w, http.StatusMethodNotAllowed, "INVALID_ARGUMENT", "Activity method is not allowed.")
		return
	}
	if h.read == nil {
		writeOperatorError(w, http.StatusInternalServerError, "INTERNAL", "Activity is unavailable.")
		return
	}
	scope, err := parseActivityScope(req)
	if err != nil {
		writeOperatorError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "Activity scope is invalid.")
		return
	}
	query, _ := url.ParseQuery(req.URL.RawQuery)
	kind := query.Get("kind")
	page, err := parseActivityPage(query, scope.Workspace)
	if err == nil {
		page.ExchangeFilter, err = parseRetainedActivityFilter(query, kind, page.Limit)
	}
	if err != nil {
		writeOperatorError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "Activity page is invalid.")
		return
	}
	activity, err := h.read(req, scope, page)
	if err != nil {
		writeOperatorError(w, http.StatusInternalServerError, "INTERNAL", "Activity is unavailable.")
		return
	}
	if kind == "exchange" {
		activity.MCPOperations = nil
	} else if kind == "mcp_operation" {
		activity.Exchanges = make([]executionevidence.ActivityExchange, 0)
	}
	writeOperatorJSON(w, http.StatusOK, activity)
}

func parseActivityScope(req *http.Request) (executionevidence.ProjectionScope, error) {
	query, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		return executionevidence.ProjectionScope{}, errors.New("invalid Activity query")
	}
	for key, values := range query {
		if (key != "workspace" && key != "kind" && key != "limit" && key != "exchange_cursor" && key != "mcp_cursor" && key != "from" && key != "to" && key != "principal" && key != "result") || len(values) != 1 {
			return executionevidence.ProjectionScope{}, errors.New("unsupported or ambiguous Activity query")
		}
		if key == "kind" && values[0] != "exchange" && values[0] != "mcp_operation" {
			return executionevidence.ProjectionScope{}, errors.New("unsupported Activity kind")
		}
	}
	workspace := query.Get("workspace")
	if workspace == "" {
		return executionevidence.ProjectionScope{Kind: executionevidence.ProjectionScopeAll}, nil
	}
	slug, err := routing.ParseWorkspaceSlug(workspace)
	if err != nil || slug.String() != workspace {
		return executionevidence.ProjectionScope{}, errors.New("exact Activity Workspace required")
	}
	return executionevidence.ProjectionScope{Kind: executionevidence.ProjectionScopeWorkspace, Workspace: slug.String()}, nil
}

func parseActivityPage(query url.Values, workspace string) (ledger.ActivityPageQuery, error) {
	page := ledger.ActivityPageQuery{Kind: query.Get("kind")}
	if _, present := query["limit"]; !present {
		if query.Has("exchange_cursor") || query.Has("mcp_cursor") {
			return page, errors.New("Activity cursor requires page limit")
		}
		return page, nil
	}
	limit, err := strconv.Atoi(query.Get("limit"))
	if err != nil || limit < 1 || limit > ledger.MaximumPageSize || strconv.Itoa(limit) != query.Get("limit") {
		return page, errors.New("invalid Activity page limit")
	}
	if (query.Has("exchange_cursor") && query.Get("exchange_cursor") == "") || (query.Has("mcp_cursor") && query.Get("mcp_cursor") == "") {
		return page, errors.New("exact Activity cursor required")
	}
	return ledger.NewActivityPageQuery(workspace, page.Kind, limit, query.Get("exchange_cursor"), query.Get("mcp_cursor"))
}
