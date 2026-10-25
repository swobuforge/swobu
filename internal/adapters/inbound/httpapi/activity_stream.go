package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/ledger"
)

// ActivityStreamHandler emits bounded now/history observations, not a lossless
// event log. Reconnect starts fresh; no event IDs or retained cursor are implied.
type ActivityStreamHandler struct {
	read     activityReadFunc
	sessions chan struct{}
}

func NewActivityStreamHandler(read activityReadFunc) ActivityStreamHandler {
	return ActivityStreamHandler{read: read, sessions: make(chan struct{}, 16)}
}

func (handler ActivityStreamHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeOperatorError(writer, http.StatusMethodNotAllowed, "INVALID_ARGUMENT", "Activity stream method is not allowed.")
		return
	}
	scope, err := parseActivityScope(request)
	query, queryErr := url.ParseQuery(request.URL.RawQuery)
	if err != nil || queryErr != nil || query.Has("limit") || query.Has("exchange_cursor") || query.Has("mcp_cursor") || query.Has("from") || query.Has("to") || query.Has("principal") || query.Has("result") || request.Header.Get("Last-Event-ID") != "" {
		writeOperatorError(writer, http.StatusBadRequest, "INVALID_ARGUMENT", "Activity stream scope or resume is invalid.")
		return
	}
	if handler.sessions == nil {
		writeOperatorError(writer, http.StatusInternalServerError, "INTERNAL", "Activity stream is unavailable.")
		return
	}
	select {
	case handler.sessions <- struct{}{}:
		defer func() { <-handler.sessions }()
	default:
		writeOperatorError(writer, http.StatusServiceUnavailable, "UNAVAILABLE", "Activity observation capacity is exhausted.")
		return
	}
	controller := http.NewResponseController(writer)
	if handler.read == nil || controller.SetWriteDeadline(time.Now().Add(5*time.Second)) != nil {
		writeOperatorError(writer, http.StatusInternalServerError, "INTERNAL", "Activity stream is unavailable.")
		return
	}
	defer controller.SetWriteDeadline(time.Time{})
	ctx, cancel := context.WithTimeout(request.Context(), time.Minute)
	defer cancel()
	request = request.WithContext(ctx)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lifetime := time.NewTimer(time.Minute)
	defer lifetime.Stop()
	started := false
	for {
		if request.Context().Err() != nil {
			return
		}
		projection, err := handler.read(request, scope, ledger.ActivityPageQuery{})
		if err != nil {
			if !started {
				writeOperatorError(writer, http.StatusInternalServerError, "INTERNAL", "Activity stream is unavailable.")
			} else {
				controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
				fmt.Fprint(writer, "event: error\ndata: {\"code\":\"activity_unavailable\"}\n\n")
				controller.Flush()
			}
			return
		}
		if query.Get("kind") == "exchange" {
			projection.MCPOperations = nil
		} else if query.Get("kind") == "mcp_operation" {
			projection.Exchanges = make([]evidence.ActivityExchange, 0)
		}
		body, err := json.Marshal(struct {
			Mode     string                      `json:"mode"`
			Complete bool                        `json:"complete"`
			Activity evidence.ActivityProjection `json:"activity"`
		}{"now_history_observation", false, projection})
		if err != nil || len(body) > 1<<20 {
			if !started {
				writeOperatorError(writer, http.StatusInternalServerError, "INTERNAL", "Activity stream projection exceeds its bound.")
			}
			return
		}
		if !started {
			writer.Header().Set("Content-Type", "text/event-stream")
			writer.Header().Set("Cache-Control", "no-store")
			writer.Header().Set("X-Accel-Buffering", "no")
			started = true
		}
		if controller.SetWriteDeadline(time.Now().Add(5*time.Second)) != nil {
			return
		}
		if _, err := fmt.Fprintf(writer, "event: activity\ndata: %s\n\n", body); err != nil {
			return
		}
		if controller.Flush() != nil {
			return
		}
		select {
		case <-request.Context().Done():
			return
		case <-lifetime.C:
			return
		case <-ticker.C:
		}
	}
}
