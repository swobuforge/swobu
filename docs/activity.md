# Activity and retained exports

Swobu's local Control API exposes live Activity observations and retained Ledger
pages. Both are **content-free**: they are not raw request or response logs.
Captured evidence can be incomplete (disabled retention, exporter lag, dropped
records or earlier software versions). A missing record never proves an
operation did not run.

## Filter retained Exchanges

`GET /api/v1/activity?kind=exchange&limit=50&workspace=engineering`

This reads the current retained page, not the live process-local snapshot. For
model Exchanges only, additional filters are `from` (inclusive settlement time),
`to` (exclusive settlement time), `principal` (exact access ID), and `result`
(terminal delivery result such as `succeeded` or `exchange_failed`). Times use
RFC3339, including an explicit offset. Filters are combined with AND.

Follow `page.exchange_next` through the `exchange_cursor` query parameter.
Cursors bind Workspace and evidence kind. They represent a position, not a
transactional snapshot or permission to read another Workspace. Other filter
values are supplied anew on each page. For MCP history, use
`kind=mcp_operation` with its separate `mcp_cursor`; the Exchange filters are
intentionally rejected for MCP until the retained MCP query supports them.

Omit `limit` to observe live/recent Activity. Retained-only filters must **not**
be combined with a live request. The finite Activity SSE stream is also a live
observation, never a lossless replay.

## Download CSV or NDJSON

`GET /api/v1/activity/export?kind=exchange&workspace=engineering&format=csv`

or

`GET /api/v1/activity/export?kind=mcp_operation&format=ndjson`

The endpoint requires an authenticated **local Node Control** context, like
other `/api/v1/activity` methods. Workspace bearer credentials cannot use it.
It uses the same retained Activity reader and output projection as the list API,
not the raw SQLite/Postgres database. No provider credentials, prompts,
completions, tool arguments or tool results are included.

- `kind` is required: `exchange` or `mcp_operation`. Each response contains
  exactly one kind, with no invented cross-kind chronology.
- `format` is `ndjson` (default) or `csv`.
- `max_rows` defaults to 500, maximum 2000 per response. The response is also
  bounded to 8 MiB and 30 seconds, with only two concurrent exports.
- Optional `workspace` scopes to one Workspace. For Exchanges, the same
  `from`, `to`, `principal` and `result` filters as retained Activity apply.
- A nonempty `X-Swobu-Next-Cursor` response header means more rows are
  available. Continue with `cursor=<header value>`, preserving all filters.
  A response without that header exhausted the observed retained page. It is
  not a guarantee of complete capture.
- `X-Swobu-Export-Rows` reports the number of rows, and
  `X-Swobu-Export-Mode: retained-page` records the completeness boundary.

Responses are buffered before committing headers. A Ledger failure does not
produce a successful but silently truncated download; oversized exports return
HTTP 413 so the caller can reduce `max_rows`. CSV sanitizes untrusted textual
cells against spreadsheet-formula execution. NDJSON carries explicit `kind`
and either `exchange` or `mcp_operation` evidence.

## UI and agent integration

Activity and Settings → History & exports use this endpoint for CSV or NDJSON.
Select one evidence kind and Workspace, then choose rows per file. Retained
Exchange settlement-time, Principal and result filters live in the URL and
apply to both the list and download. List/detail navigation preserves them;
changing filters resets continuation. MCP and live views clear Exchange-only
filters. Live snapshots cannot be downloaded as retained history.

Download first page starts at the newest matching retained row, independently
of list pagination. Download next page explicitly follows the returned export
cursor with the same filters. Downloads are never retried or aggregated
automatically; errors save no file. Settings exposes the same download controls,
not retention or OpenTelemetry configuration. Do not label a collection of
pages an atomic backup. MCP `activity_get` already supports bounded retained
paging for agents; the MCP tool need not return megabytes of export content or
create a new tool grafting policy.
