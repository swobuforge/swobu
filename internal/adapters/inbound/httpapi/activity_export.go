package httpapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/routing"
)

// The accepted export envelope protects local Control with two concurrent
// retained scans and a 30-second query deadline. Successful response bodies
// are capped at 8 MiB; this does not bound transient row serialization memory.
// Capacity returns 503 and oversized bodies return 413 before success.
// These are explicit product bounds, not a Ledger retention policy.
const (
	activityExportDefaultRows = 500
	activityExportMaxRows     = 2000
	activityExportMaxBytes    = 8 << 20
)

// ActivityExportHandler downloads bounded, content-free retained Activity. It
// uses the same history reader and cursor authority as Activity and Admin MCP.
// An export is an observed retained slice, not an atomic database snapshot or
// a guarantee that the Ledger captured every attempted operation.
type ActivityExportHandler struct {
	read  activityReadFunc
	slots chan struct{}
}

func NewActivityExportHandler(read activityReadFunc) ActivityExportHandler {
	return ActivityExportHandler{read: read, slots: make(chan struct{}, 2)}
}

type activityExportQuery struct {
	scope  evidence.ProjectionScope
	kind   string
	format string
	rows   int
	cursor string
	filter ledger.ExchangeFilter
}

func parseActivityExportQuery(req *http.Request) (activityExportQuery, error) {
	var export activityExportQuery
	values, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		return export, errors.New("invalid export query")
	}
	for key, entries := range values {
		if len(entries) != 1 || (key != "workspace" && key != "kind" && key != "format" && key != "max_rows" && key != "cursor" && key != "from" && key != "to" && key != "principal" && key != "result") {
			return export, errors.New("unsupported or ambiguous export query")
		}
	}
	export.kind = values.Get("kind")
	if export.kind != "exchange" && export.kind != "mcp_operation" {
		return export, errors.New("one export evidence kind is required")
	}
	export.scope = evidence.ProjectionScope{Kind: evidence.ProjectionScopeAll}
	if values.Has("workspace") {
		workspace := values.Get("workspace")
		slug, err := routing.ParseWorkspaceSlug(workspace)
		if err != nil || slug.String() != workspace {
			return export, errors.New("invalid export Workspace")
		}
		export.scope = evidence.ProjectionScope{Kind: evidence.ProjectionScopeWorkspace, Workspace: workspace}
	}
	export.format = values.Get("format")
	if export.format == "" {
		if values.Has("format") {
			return export, errors.New("export format cannot be empty")
		}
		export.format = "ndjson"
	}
	if export.format != "ndjson" && export.format != "csv" {
		return export, errors.New("unsupported export format")
	}
	export.rows = activityExportDefaultRows
	if values.Has("max_rows") {
		export.rows, err = strconv.Atoi(values.Get("max_rows"))
		if err != nil || export.rows < 1 || export.rows > activityExportMaxRows || strconv.Itoa(export.rows) != values.Get("max_rows") {
			return export, errors.New("export max_rows must be an integer between 1 and 2000")
		}
	}
	export.filter, err = parseRetainedActivityFilter(values, export.kind, 1)
	if err != nil {
		return export, err
	}
	export.cursor = values.Get("cursor")
	if values.Has("cursor") && export.cursor == "" {
		return export, errors.New("export cursor cannot be empty")
	}
	// Fail before querying history for a malformed cursor, including a cursor
	// copied from a different Workspace or a different kind of evidence.
	_, err = activityExportPage(export, 1)
	return export, err
}

func activityExportPage(export activityExportQuery, limit int) (ledger.ActivityPageQuery, error) {
	var page ledger.ActivityPageQuery
	var err error
	if export.kind == "exchange" {
		page, err = ledger.NewActivityPageQuery(export.scope.Workspace, export.kind, limit, export.cursor, "")
	} else {
		page, err = ledger.NewActivityPageQuery(export.scope.Workspace, export.kind, limit, "", export.cursor)
	}
	page.ExchangeFilter = export.filter
	return page, err
}

func (handler ActivityExportHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeOperatorError(writer, http.StatusMethodNotAllowed, "INVALID_ARGUMENT", "Activity export method is not allowed.")
		return
	}
	query, err := parseActivityExportQuery(request)
	if err != nil {
		writeOperatorError(writer, http.StatusBadRequest, "INVALID_ARGUMENT", "Activity export query is invalid.")
		return
	}
	if handler.read == nil || handler.slots == nil {
		writeOperatorError(writer, http.StatusServiceUnavailable, "UNAVAILABLE", "Activity export is unavailable.")
		return
	}
	select {
	case handler.slots <- struct{}{}:
		defer func() { <-handler.slots }()
	default:
		writeOperatorError(writer, http.StatusServiceUnavailable, "UNAVAILABLE", "Activity export capacity is exhausted.")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()
	body, total, next, err := handler.render(request.WithContext(ctx), query)
	if err != nil {
		if errors.Is(err, errActivityExportTooLarge) {
			writeOperatorError(writer, http.StatusRequestEntityTooLarge, "EXPORT_TOO_LARGE", "Reduce max_rows and retry the export.")
			return
		}
		writeOperatorError(writer, http.StatusServiceUnavailable, "UNAVAILABLE", "Activity export could not be completed within its resource bounds.")
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="swobu-activity-%s%s.%s"`, query.kind, exportFilenameWorkspace(query.scope), query.format))
	writer.Header().Set("X-Swobu-Export-Rows", strconv.Itoa(total))
	writer.Header().Set("X-Swobu-Export-Mode", "retained-page")
	if next != "" {
		writer.Header().Set("X-Swobu-Next-Cursor", next)
	}
	if query.format == "csv" {
		writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
	} else {
		writer.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	}
	writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func exportFilenameWorkspace(scope evidence.ProjectionScope) string {
	if scope.Kind == evidence.ProjectionScopeWorkspace {
		return "-" + scope.Workspace
	}
	return ""
}

var errActivityExportTooLarge = errors.New("Activity export exceeds bounded response size")

type activityExportRow struct {
	Kind         string                     `json:"kind"`
	Exchange     *evidence.ActivityExchange `json:"exchange,omitempty"`
	MCPOperation *evidence.MCPOperation     `json:"mcp_operation,omitempty"`
}

func (handler ActivityExportHandler) render(req *http.Request, query activityExportQuery) ([]byte, int, string, error) {
	var data bytes.Buffer
	var csvWriter *csv.Writer
	if query.format == "csv" {
		csvWriter = csv.NewWriter(&data)
		if err := csvWriter.Write(activityExportCSVHeaders); err != nil {
			return nil, 0, "", err
		}
		csvWriter.Flush()
		if err := csvWriter.Error(); err != nil {
			return nil, 0, "", err
		}
	}
	count, next := 0, ""
	for count < query.rows {
		if err := req.Context().Err(); err != nil {
			return nil, 0, "", err
		}
		limit := min(ledger.MaximumPageSize, query.rows-count)
		page, err := activityExportPage(query, limit)
		if err != nil {
			return nil, 0, "", err
		}
		result, err := handler.read(req, query.scope, page)
		if err != nil || result.Page == nil || result.Page.Mode != "retained" || result.Scope != query.scope {
			return nil, 0, "", errors.New("retained Activity page unavailable")
		}
		var rows []activityExportRow
		if query.kind == "exchange" {
			if len(result.MCPOperations) != 0 {
				return nil, 0, "", errors.New("retained Activity kind mismatch")
			}
			for i := range result.Exchanges {
				rows = append(rows, activityExportRow{Kind: query.kind, Exchange: &result.Exchanges[i]})
			}
			next = result.Page.ExchangeNext
		} else {
			if len(result.Exchanges) != 0 {
				return nil, 0, "", errors.New("retained Activity kind mismatch")
			}
			for i := range result.MCPOperations {
				rows = append(rows, activityExportRow{Kind: query.kind, MCPOperation: &result.MCPOperations[i]})
			}
			next = result.Page.MCPNext
		}
		if len(rows) > limit || (next != "" && (len(rows) == 0 || next == query.cursor)) {
			return nil, 0, "", errors.New("retained Activity pagination is inconsistent")
		}
		for _, row := range rows {
			if csvWriter != nil {
				if err := csvWriter.Write(activityExportCSVRow(row)); err != nil {
					return nil, 0, "", err
				}
				csvWriter.Flush()
				if err := csvWriter.Error(); err != nil {
					return nil, 0, "", err
				}
			} else {
				encoded, err := json.Marshal(row)
				if err != nil {
					return nil, 0, "", err
				}
				data.Write(encoded)
				data.WriteByte('\n')
			}
			if data.Len() > activityExportMaxBytes {
				return nil, 0, "", errActivityExportTooLarge
			}
			count++
		}
		if next == "" || len(rows) == 0 {
			break
		}
		query.cursor = next
	}
	if err := req.Context().Err(); err != nil {
		return nil, 0, "", err
	}
	return data.Bytes(), count, next, nil
}

var activityExportCSVHeaders = []string{
	"kind", "id", "workspace", "principal", "started_at", "settled_at", "duration_ms", "state", "operation", "route", "provider", "model", "source", "tool", "http_status", "attempts", "connection_id", "connection_revision", "input_tokens", "output_tokens", "reasoning_tokens", "cache_read_tokens", "cache_write_tokens", "execution_possibility",
}

func activityExportCSVRow(row activityExportRow) []string {
	fields := make([]string, len(activityExportCSVHeaders))
	fields[0] = row.Kind
	if record := row.Exchange; record != nil {
		fields[1], fields[2], fields[3] = record.ExchangeID, record.Workspace, record.AccessID
		fields[4] = record.StartedAt.UTC().Format(time.RFC3339Nano)
		if record.DurationMS != nil {
			fields[6] = strconv.Itoa(*record.DurationMS)
		}
		fields[7], fields[8], fields[9] = record.State, record.Operation, record.Route
		fields[10], fields[11] = record.Provider, record.Model
		fields[14], fields[15] = strconv.Itoa(record.HTTPStatus), strconv.Itoa(record.AttemptCount)
		fields[16], fields[17] = record.ConnectionID, strconv.FormatUint(record.ConnectionRevision, 10)
		if record.TokenUsage != nil {
			for j, value := range []*int{record.TokenUsage.InputTokens, record.TokenUsage.OutputTokens, record.TokenUsage.ReasoningTokens, record.TokenUsage.CacheReadTokens, record.TokenUsage.CacheWriteTokens} {
				if value != nil {
					fields[18+j] = strconv.Itoa(*value)
				}
			}
		}
	}
	if record := row.MCPOperation; record != nil {
		fields[1], fields[2], fields[3] = record.OperationID, record.Workspace, record.AccessID
		fields[4] = record.StartedAt.UTC().Format(time.RFC3339Nano)
		if !record.SettledAt.IsZero() {
			fields[5] = record.SettledAt.UTC().Format(time.RFC3339Nano)
		}
		fields[7], fields[12], fields[13], fields[23] = string(record.Outcome), record.Source, record.Tool, string(record.Occurrence)
	}
	for i := range fields {
		fields[i] = safeActivityCSVCell(fields[i])
	}
	return fields
}

// Spreadsheet applications may evaluate remote provider/tool names as formulas.
// A leading literal apostrophe prevents that without modifying NDJSON evidence.
func safeActivityCSVCell(raw string) string {
	trimmed := strings.TrimLeft(raw, " \t\r\n")
	if trimmed != "" && strings.ContainsAny(trimmed[:1], "=+-@") {
		return "'" + raw
	}
	return raw
}
