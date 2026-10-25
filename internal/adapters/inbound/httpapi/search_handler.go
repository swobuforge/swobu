package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/search"
)

func NewWorkspaceSearchHandler(service search.Service, authorize func(*http.Request, string) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspace := r.PathValue("workspace")
		if (r.Method != http.MethodPost && r.Method != http.MethodGet) || authorize == nil || !authorize(r, workspace) {
			http.NotFound(w, r)
			return
		}
		var query search.Query
		if r.Method == http.MethodGet {
			// GET and POST share invocation semantics. Reject ambiguous query values
			// before dispatch; URLs cannot select another upstream or credential.
			params, err := url.ParseQuery(r.URL.RawQuery)
			valid := err == nil
			for key, values := range params {
				if (key != "query" && key != "limit") || len(values) != 1 {
					valid = false
				}
			}
			query.Text = params.Get("query")
			if values, ok := params["limit"]; ok {
				query.Limit, err = strconv.Atoi(values[0])
				valid = valid && err == nil
			}
			if !valid {
				http.Error(w, "invalid search request", http.StatusBadRequest)
				return
			}
		} else if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&query); err != nil {
			http.Error(w, "invalid search request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Cache-Control", "no-store")

		results, err := service.Search(r.Context(), workspace, query)
		if errors.Is(err, search.ErrNotConfigured) {
			http.Error(w, "operation not configured", http.StatusNotImplemented)
			return
		}
		if errors.Is(err, accounting.ErrUnknownPrice) || errors.Is(err, accounting.ErrBudgetExceeded) {
			http.Error(w, err.Error(), http.StatusPaymentRequired)
			return
		}
		if err != nil {
			http.Error(w, "search request failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Results []search.Result `json:"results"`
		}{Results: results})
	})
}
