# Local Web Operator source

This package owns browser navigation, presentation, server-state projections and
local interaction state. Go application commands own product semantics. B01
implements the accepted shell; B02 adds generated API transport; W01 cuts over
the embedded bundle. Production pages never import test fixtures.

Workspace Connect requires a concrete model ID and checks the saved production
matcher before review. Model changes discard the old review. Apply carries that
ID in the original retained intent. Claude Code and Antigravity reviews explain
that endpoint/discovery configuration still needs explicit client model selection.

Workspace Connect publishes/unpublishes the built-in relay through the mounted
publication API. Each explicit mutation binds the strong revision returned by
the Workspace publication read. A stale or uncertain response requires inspecting
current state before a new action. Publication is reachability only; existing
Workspace credentials, Services, permissions and limits still govern requests.
Settings Network shows the managed relay URL without asserting reachability.
Connect derives enabled-Service addresses from the same publication query and
removes remote rows on unpublish. Only a valid HTTPS publication origin supplies
remote URLs; local addresses remain separate. Models exposes both the unversioned
Codex/Claude root and OpenAI-compatible `/v1`; MCP uses `/mcp`, Search `/search`.
Copy actions contain public URLs only, with explicit clipboard-failure feedback.

Use Bun 1.3.13 (`.bun-version` and `packageManager`), with the single `bun.lock`:

- `bun install --frozen-lockfile`
- `bun run typecheck`
- `bun run lint`
- `bun run test`
- `bun run build`
- `bun run e2e` (install the pinned Playwright Chromium first with `bun x playwright install chromium`)

Build output goes to ignored `.staging/`. It is not served by the Go binary in
B01. `internal/operatorweb/assets/` remains generated production authority until
W01; never copy prototype stores or decompile old JavaScript into this source.

Model route saves bind the Workspace ETag and return the updated Workspace.
After uncertain delivery, inspect the Workspace before another change; model
routes and MCP mounts have no replay intent or receipt journal. MCP mount
preview and apply share the alias-owned PUT address; apply carries the exact
Workspace ETag and owner review. After a failed or uncertain apply, read current
Workspace state before reviewing another mutation.

Models authoring preserves server-projected route order. Route identity is its
Go `path.Match` pattern; the browser never evaluates globs. Priority edits remain
local until Apply, bind the reviewed Workspace ETag and lock after a stale result
until Discard reloads current state. Try model ID queries saved production
resolution. Duplicate patterns cannot create a second route or overwrite an
existing route through the create dialog. Slash patterns use one encoded URL
segment, including direct document reloads; data-plane path admission is unchanged.

`src/app/router.tsx` owns the typed address space. URL state owns navigation;
TanStack Query owns server resources; mounted components own drafts and finder
state. Native dialog owns focus containment and Escape restoration. No browser
persistence or global resource fixture store exists.

Authority: accepted closeout RFC 006 and the Operator brief in
`docs/04-design/web/surfaces/operator.md`. Reproduce the accepted shell at
1440/390 and both sides of 760px. Browser screenshots under `test-results/` are
candidate evidence until compared with accepted references. Shell checks do
not qualify resource workflows or real-client behavior.

B02 build entrypoints (also available from the platform root):

- `make webui-generate`: generated OpenAPI types, typecheck, Vite staging build,
  asset hashes and source checksum in `.staging/manifest.json`.
- `make webui-verify`: check generated contracts, rebuild into a temporary
  directory and compare every output byte against staging without rewriting it.
- `make webui-test`: typecheck, lint and unit contracts.
- `make webui-e2e`: browser journeys; `make webui-visual`: frozen-shell captures.

Both generation and verification use `scripts/build.ts`. The source checksum
covers app source, build scripts, package/config files, Bun lock and OpenAPI.
The staged bundle scan rejects legacy endpoint and fixture markers. These lanes
stage output only; W01 owns production embed cutover. Generated API schema lives
in `src/api/generated/`; never edit it by hand. The transport consumes generated
Problem types; success data remains unknown until domain cards provide schemas.

`createControlClient` is the sole request entrypoint. openapi-fetch consumes the
generated path/method/parameter contract and delegates to createControlTransport.
The transport enforces same-origin cookie requests, exact revision/retry headers,
no redirects and no-store success responses before exposing bodies. It decodes
canonical Problems as ControlRequestError and never retries. Do not introduce a
parallel resource fetch wrapper. Domain success types arrive with concrete schemas.

Browser shell checks now exercise Vite production preview of verified staging.
The same browser lane also starts an isolated real daemon and proves launch,
session, authenticated control and replay denial against its current embed. `make webui-check-embed` compiles staging into an isolated Go
embed.FS and verifies every manifest hash without replacing production assets.

W01 source-to-embed and resource controls:

- `bun run api:generate` is mandatory after the Workspace read/create/rename
  contract change. Do not edit `src/api/generated/schema.ts` manually.
- `make webui-generate && make webui-verify && make webui-check-embed`
  qualifies the source output, with a pinned Go 1.25.10 toolchain.
- `make webui-cutover` replaces the old embedded assets with the verified
  build; rebuild the Go binary afterward. The source is the WebUI package,
  never the historical compiled bundle or the design prototype.
- Workspace and Connection pages consume live, no-store Node Control reads.
  Writes are explicit, revision-bound where applicable, and never retried
  automatically. The remaining Access, Limits, Connect and Activity workflows
  do not claim to be implemented.

W03 live activation/evidence source:

- Workspace Connect detects *installed local* clients via `GET /api/v1/clients` and
  previews exact client-file edits via `POST /api/v1/client-configurations?dry_run=true`.
  An existing Principal with current Models permission is selected explicitly.
  Preview validates the configured credential without issuance or exposing keys.
  Confirmed apply reuses a valid binding or issues once through Access and installs
  the credential using the client adapter. Configured requires exact credential
  readback accepted for the intended Workspace Models service without inference.
  Apply requires retained review, explicit user approval, the exact revision,
  and one stable retry key. Ambiguous outcomes are never silently replayed.
- Activity consumes actual Exchange/MCP evidence via `GET /api/v1/activity` and
  `/api/v1/activity/operations/{id}`. All-kind view is a bounded snapshot;
  retained pages are selected by kind with server-provided cursors. Workspace
  reads do not imply Node-wide Ledger health. No prompts or payloads are shown.
- Retained Exchange settlement-time, Principal and result filters are URL-owned;
  Apply commits the draft together and resets pagination. Detail/back/older links
  preserve committed filters; MCP/live views clear Exchange-only filters.
  Activity and Settings History share bounded CSV/NDJSON downloads through
  `/api/v1/activity/export`. First-page export starts independently of the list
  cursor; subsequent files require explicit continuation with unchanged filters.
  The client validates download metadata, caps retained chunks at 8 MiB and
  aborts on unmount. File bodies never enter React Query's mutation cache;
  successful mutations return only row count and continuation. It never retries
  or combines files automatically. Retention
  and OTel configuration remain unavailable. See `tests/e2e/activity-history.spec.ts`
  for responsive/navigation/failure proof and `first-route.spec.ts` for real
  embedded SQLite downloads after a model request. `node-transaction.spec.ts`
  downloads retained MCP evidence after an actual Workspace tool call.
- These installed-local-client APIs are **not** the unimplemented managed
  `client-setups`, universal `client-adapters`, or paid route verification
  contracts. Updating client files is not evidence of an actual client request.
- After applying this source, regenerate `src/api/generated/schema.ts` with
  pinned Bun from the updated Go OpenAPI before full typecheck/browser proof.

Principal Service request ceilings are edited on Workspace Access through the
existing reviewed grant flow. Only selected Services get RPM inputs; blank
means unlimited. Values must be whole numbers from 1 to 100,000. Preview and
apply bind the exact policy, relationship revision and retry identity. Stale
consent requires inspection and a fresh review. Rates cover live operations
shared across credentials, not tokens or dollars; restart restores bucket credit
while retaining policy.

Workspace Overview follows Services → bounded recorded Activity → policy. Service
rows link directly to their authoring pages and use server-projected topology.
The access metric counts active Principals with current Workspace use grants;
publication describes relay addressing without asserting verified availability.
Unavailable projections remain explicit. Managed rename lives in the Workspace
header and uses the existing revision-bound mutation. Browser coverage in
`tests/e2e/resources.spec.ts` exercises these summaries and responsive seams.

Workspace Limits reads and writes the generated `/service-limits` Control API at
the exact strong Workspace ETag. Each enabled Service has an explicit Unlimited
choice or positive ceiling; Models alone supports concurrency. Failed or uncertain
writes lock the draft until an explicit read of current policy, without automatic
retry. Window focus/reconnect does not replace an active draft. Estimated USD
spend remains an independent editor and authority. Rate credit and active exchanges
are process-local; restarting the Node resets them, without resetting spend.

Direct Search authoring keeps one primary and up to seven ordered fallback
Connections in a local draft. Save replaces the complete chain at the reviewed
Workspace ETag; stale, forbidden or lost acknowledgements lock edits until an
explicit current-state read. Unavailable saved Connections remain visible.

Connection maintenance (21 October 2026): the detail page now exposes the
existing revision-bound `PATCH /connections/{id}` and durable
`POST /connections/{id}/credential-replacements` operations for managed Nodes.
Editing is **complete replacement authoring**, not a partial/redacted readback
round-trip: operators explicitly supply endpoint, credential locators, custom
headers, and stdio argv/environment again because the GET projection withholds
private data. An unchanged-external-identity acknowledgement is mandatory.
Credential rotation is a separate write-only operation and does not rebuild
Workspace references. Only a manual exact replay with the original key,
revision and material is offered after an uncertain result. On stale edits,
explicit current-state read is required. Pending and uncertain replacement fields
remain locked so manual replay cannot change the submitted material. Private
mutation closures are discarded when the dialog unmounts.

`tests/e2e/connection-lifecycle.spec.ts` exercises the real embedded Operator,
two unchanged Workspace consumers, lost-acknowledgement replay, next-request
credential use, and redacted reads/Activity/Admin MCP. Resource browser tests
cover stale/unmanaged behavior, stdio selection, pending locks and optional
endpoint editing. The shared Modal focuses `data-autofocus` after opening and
restores the invoking control on close, including React effect replay. Connection
header actions wrap together before squeezing the identity heading.

Contextual Connection creation uses `src/app/connection-create.tsx` from both
Connections and Workspace authoring. Models targets, MCP sources and Search
primary/fallback actions fix the Connection kind and attach the returned identity
only to the preserved local draft. Route saves remain explicit. A new MCP source
clears selected tools and authority review, discovers its actual catalog, and
requires a new review/apply. Standalone creation still opens Connection detail.
Creation does not verify upstream reachability or credential availability.

Creation fields lock while pending and after uncertain delivery. Manual replay
retains the same material and idempotency key; a definitive validation rejection
permits correction. Closing discards the local retry intent and explicitly directs
the operator to inspect Connections before another creation. The shared native
Modal owns nested focus containment and restoration. Contextual browser proof
covers preserved drafts, explicit saves, cancellation, recovery and 390/1440px;
`first-route.spec.ts` proves embedded managed-Node routing and retained Activity.

Manual client setup (21 October 2026): Workspace Connect now offers copyable,
secret-free examples for Codex, Claude Code, OpenAI-compatible HTTP, Streamable
HTTP MCP, and Direct Search. Recipes are derived only from `workspaceEndpoints`
(local listener or current published HTTPS origin) and saved Service availability.
A concrete Models ID is accepted only after a successful server model-match
check, keyed to the currently observed Workspace revision. Pending or failed
rechecks hide examples even when the query cache retains an earlier match.
Remote examples likewise require a successful current publication read, so a
failed refresh cannot expose a cached published address. No remote client file is mutated and no
Workspace credential is read, issued or serialized by recipe generation. Codex
TOML and Claude JSON are merge examples, not whole-file replacements; generic MCP
connection fields must be adapted to the actual client schema. HTTP examples
make real billable requests. The existing reviewed local Connect remains the
only client-file mutation path. See `tests/e2e/manual-client-setup.spec.ts`.

Activity recovery keeps recorded upstream invocation counts separate from client
delivery. The detail reader validates counts against invocation facts; unknown or
contradictory execution evidence fails the read. Route and Connection links open
current resources while recorded revisions remain historical evidence. Missing
records never prove no execution, and this view never authorizes replay or infers
provider charges.
