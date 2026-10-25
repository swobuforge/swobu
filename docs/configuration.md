# Workspace Configuration

Swobu stores Workspace routing, reusable upstream Connections, Principal credentials/grants, Workspace ingress publications, and estimated-spend limit policy together in `swobu.yaml`. The running daemon is the only managed writer; change configuration through its local management API or CLI. Manual edits require stopping the daemon first.

Swobu assumes the local host is trusted, including processes that can reach
its loopback Control listener. New managed files and directories request `0600`
and `0700` permissions where supported; existing user-selected paths are not
rejected or rewritten because of their ACLs. OS-user separation alone does not
isolate local Control: use a separate host or network-isolated container when
untrusted local processes are present.
See [Local trust and privacy](local-trust-and-privacy.md).

```yaml
schema_version: 5
workspaces: {}
connections:
  schema_version: 1
  connections: []
principals:
  schema_version: 3
  access: []
publications:
  schema_version: 2
  generation: 1
  workspaces: []
  audits: []
accounting:
  schema_version: 1
  limits: {}
```

The `connections`, `principals`, `publications` and `accounting` sections are optional when empty; managed actions materialize them only when needed. Non-empty `principals` contains typed Principal identities, scoped Workspace permissions and **credential digests only**, never issued bearer tokens. Non-empty `publications` holds only sorted, unique Workspace slugs reachable through the built-in relay. Neither section is a JSON string.

The `connections` section is optional when empty. A Connection has one stable ID and a typed provider specification. Workspace model targets reference it through `connection_id`, and MCP mounts and Workspace Search routes refer to Connections through `connection_id`. The Connection document is stored as ordinary YAML data, not as a JSON string or a separate file. Credentials remain referenced by locator, not embedded as secret values.

`swobu config validate --config ./swobu.yaml` reads one immutable Node revision and checks routing, the Connection graph, Principal/Workspace grants, publication references, accounting policy and the runtime accounting journal. It creates no files, modifies no permissions, contacts no providers and does not start the daemon. All five configuration sections commit through one atomic file replacement; interrupted writes preserve the last completed version. To edit the document manually, stop the managed daemon, preserve every section and restart it.

Accounting limit **policy** belongs in `accounting.limits` (USD cents, `day` or `month`, `stop_new_billable_work` or `observe_only`, with explicit `unknown_price: reject`). The `accounting.json` file is now only a durable **runtime journal** of reservations, settlement corrections and clock floor. It may be absent when no provider work occurred. Existing `accounting.json` files containing `limits` or `limit_changes` are rejected at startup: explicitly translate their policy into YAML; never silently discard in-flight reservations. Approved limit-change receipts currently remain atomically co-located in `accounting.limit_changes` to protect command replay; their later evidence separation must preserve idempotency.


For externally managed or read-only mounted configuration, start `swobu daemon --config ./swobu.yaml --config-read-only`. The daemon uses existing configuration without taking a writable config lock or modifying YAML, and rejects management mutations before side effects. Normal authorized Workspace data-plane operations and durable execution evidence remain available. Stop/restart to consume a changed document; read-only mode does not watch for edits.

In this mode, **runtime evidence does not share the configuration mount**. It lives under the OS state root (`SWOBU_HOME/state/external-nodes/<stable-node-id>` when `SWOBU_HOME` is set); `relay.json`, the accounting journal, retained SQLite Activity Ledger (`ledger.sqlite3`),
and temporary auth/connect attempts are kept there. Managed Nodes retain their
existing `<config-stem>.ledger.sqlite3` beside YAML. The stable ID comes from the canonical config path, so symlink aliases do not split a Node's accounting state. Keep the runtime root writable and durable: changing the mount path can change the Node identity. Offline validation for this mode is `swobu config validate --config ./swobu.yaml --config-read-only`. Switching between managed and externally managed modes with existing accounting, relay or SQLite Ledger files **fails closed**; explicitly transfer them with the daemon stopped and verify the destination before changing mode. There is no automatic transfer or shadow-reader fallback.

There is no standalone Principal authority file, historical permission import or startup conversion. Only the current Principal document schema is accepted.

Clients use `/workspaces/dev/...` and request a model ID (for example `coding`, `claude-sonnet-4`, or `deepseek/chat`) as `model`. Each route's **name is its matching pattern**, using Go `path.Match` (case-sensitive, entire model ID; `*` and `?` do not cross `/`; character classes and escapes are supported). Routes are evaluated **in persisted top-to-bottom order**, and the first matching route wins. Example patterns are `coding` (one virtual model), `claude-sonnet-*` (a family), `deepseek/*` (a namespaced family), and `*` (unqualified catch-all). A model ID with no match fails; a provider failure uses only the winning route's eligible fallback tiers, never another matching route. Routes with glob metacharacters are not advertised as concrete model IDs in `/models`. Missing or blank model IDs fail; `default` is ordinary text and matches only if a configured pattern matches it. Tier 1 is preferred, later tiers are fallbacks, and targets within each tier are balanced peers.

The schema-5 `routes` sequence is the only persisted representation of first-match priority. Each entry owns a `pattern` and `tiers`; its position determines priority. There is no route map, `route_order`, default-route pointer or implicit sorting. Invalid or duplicate patterns fail when opening the document. The Operator changes priority with a single ETag-checked operation (`PATCH /api/v1/workspaces/{slug}/model-routes` with `{"order":[...]}`) and checks resolution through `GET /api/v1/workspaces/{slug}/model-match?model=...`. Reordering does not change target versions.

For example:

```yaml
workspaces:
  engineering:
    routes:
      - pattern: coding
        tiers:
          - targets:
              - id: primary
                version: 1
                model: gpt-5
                protocol: responses
                connection_id: cn_openai
      - pattern: 'claude-*'
        tiers:
          - targets:
              - id: sonnet
                version: 1
                model: claude-sonnet-4-5
                protocol: messages
                connection_id: cn_anthropic
```

## Direct Search fallbacks

Search is configured independently of Models and MCP. A Workspace Search route
uses one primary Search Connection followed by up to seven ordered fallback
Connections. All entries are existing typed Search Connections; no provider
configuration or credentials are copied into the Workspace:

```yaml
workspaces:
  research:
    routes: []
    enabled_services: [search]
    search:
      connection_id: cn_primary
      fallback_connection_ids: [cn_backup, cn_last]
```

New requests resolve each attempted Connection's current revision and
credential. A failed upstream attempt proceeds to the next Connection;
a successful response with zero results is terminal. Cancellation or a budget
reservation/settlement failure stops the chain. Every actual upstream attempt
is separately accounted and recorded without query text. An unavailable
Connection can be bypassed by a later fallback, but a missing Connection
reference cannot be persisted. An existing Connection cannot be deleted while
referenced as either a primary or fallback.

Invoke with `GET /workspaces/{slug}/search?query=...&limit=10` and a current
Workspace bearer credential, locally or through a published relay. Existing
`POST` JSON requests use the same executor. Each form counts one client request
against Principal and Workspace Search ceilings, regardless of fallback count.

The local `GET /api/v1/search-evidence` projection retains the existing bounded
512-event Search head across clean restarts, including exact attempt Connection
revisions. It reports `retention_error` if history cannot be opened or saved.
Snapshots are asynchronous: abrupt death can lose pending facts, and the retained
head is not a complete audit. Query text, results and credentials are excluded.
Corrupt history is preserved for operator recovery.

Read the route with `GET /api/v1/workspaces/{slug}/search-route` and replace
it with `PUT` using the current Workspace ETag and a JSON body containing
`connection_id` and `fallback_connection_ids`. Clearing uses `DELETE`. Admin
MCP exposes the same checked mutation as `search_route_set`.

## Workspace-wide operational admission

Each Workspace can set independent traffic ceilings for Models, MCP and Search,
plus a Models concurrency ceiling. These belong to the Workspace's normal YAML
value, not the durable USD spend journal and not a Principal permission grant:

```yaml
workspaces:
  engineering:
    routes: []
    service_limits:
      models:
        requests_per_minute: 120
        concurrent: 8
      mcp:
        requests_per_minute: 60
      search:
        requests_per_minute: 30
```

Requests/minute must not exceed 100000, and Models concurrency must not exceed
10000. These are product configuration bounds, not capacity guarantees. MCP and
Search do not support concurrency ceilings.

An omitted `service_limits` section, or any zero limit, means unlimited. These
ceilings aggregate all admitted Principals and credentials for one Workspace,
across local and relay traffic. Principal-specific request limits remain separate
and apply alongside them. Models uses the request boundary (including each
`response.create` over WebSocket); Search uses the request boundary. MCP counts
actual `tools/call` invocations, including calls composed inside a Model request,
but does **not** charge `initialize` or `tools/list`. Requests admitted before a
limit edit finish normally; new admissions use the latest policy. Token credit
is process-local and resets on daemon restart; this is operational throttling,
**not** a durable spend cap or a guarantee about provider billing.

Read and edit through `GET`/`PUT /api/v1/workspaces/{slug}/service-limits` with
the current Workspace `If-Match` ETag for writes (428 missing, 412 stale).
Read-only configuration refuses writes. An Admin MCP client mounted in an
ordinary Workspace can call `workspace_service_limits_set` with the same
Workspace ETag and typed Service policy; it does not gain separate authority.
The existing `/api/v1/workspaces/{slug}/limits` endpoints continue to manage
estimated USD spending independently.

The current schema is intentionally strict: schema-4 configurations and route mappings are rejected rather than translated at startup. There are no installed customers requiring compatibility.

Swobu listens on `127.0.0.1:7926` by default. Both the Web Operator launcher and the
foreground daemon accept the same startup controls:

```sh
swobu --addr 127.0.0.1:9000 --config ./swobu.yaml
swobu daemon --addr 127.0.0.1:9000 --config ./swobu.yaml
```

`--addr` overrides `SWOBU_ADDR`; `--config` overrides `SWOBU_CONFIG_PATH`.
When bare `swobu` must start the daemon, it uses those resolved values and then
opens the authenticated Web Operator against the same address. Address and config path are
restart-bound startup configuration, not routing state, and workspace edits
never rewrite them.

Each target references a reusable `Connection` by `connection_id`; see the
[backend recipes](./README.md#backend-recipes) for setup examples. Credentials
are owned by the Connection and are never embedded in routing state. Set the protocol
as shown in the provider recipe, or configure it from the Web Operator under Connections and Workspace Services.

Swobu validates the configuration at startup and saves changes atomically.
Only one daemon can use a configuration file at a time. A directory-sync
warning means the change was saved, but its persistence through a system crash
is uncertain. Check the underlying filesystem; the daemon remains writable.
