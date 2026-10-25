# Canonical HTTP contract

`internal/adapters/inbound/httpapi/apischema` owns the **mounted local Control
contract**. `make openapi-generate` deterministically emits `openapi.json`, and
`make check-openapi` rejects drift without writing. The operation table is not a
roadmap: no unmounted target-only method can appear in exported OpenAPI or the
generated Web client. Future operations remain in the active closeout backlog
until a real handler and a concrete success contract exist.

The October 22 cutover removed 32 speculative operations and documented five
previously underspecified mounted operations, leaving 61 exported method/path
pairs. Workspace authority is expressed by actual `/permissions/{id}` reads,
reviewed writes and revocation; there is no parallel Workspace `/grants` API,
Node-admin credential universe, or Workspace-scoped secret browser. Credential
issuance uses the mounted Principal credential lifecycle. Browser launch and
exchange use a separate native/local browser admission; Activity SSE is a
bounded observation, not replayable execution evidence.

The exported table is the **qualified subset** of the local Control listener,
not a claim that every auxiliary diagnostic or internal-only route is in this
client SDK. Route admission is separately evidenced by the reviewed mux/handler
inventory, browser middleware and negative assembled daemon tests. Future
routes are promoted only with one implemented owner, typed responses and
rejection proof; never by adding a placeholder DTO or an optimistic client path.

The shared Problem Details fields are checked against Go wire types. Revision
comparison and durable idempotency remain application transaction responsibilities;
OpenAPI header declarations cannot prove those guarantees.

`make api-inventory-generate` captures HTTP mux registration expressions and
legacy namespace source consumers; `make check-api-inventory` rejects baseline
drift. Every legacy item has a closeout deletion owner. These checks run through
the package's routine `check` target. Review before regenerating: refreshing a
baseline does not authorize new legacy behavior.

The Workspace HTTP constructor has one canonical data root; its mux
registration is checked without executing domain commands. Namespace references include canonical clients; occurrence counts and evidence hashes detect new calls even
inside an already-listed file. Vendor/generated artifact directories are excluded
explicitly, while source api directories remain included.

Handler method switches are not yet fully resolved. The inventory is a source
seam check, not exhaustive mounted-operation or black-box reachability proof.
Middleware dispatch literals are included separately from mux registrations.
Their assembled admission tests own reachability; literals alone do not prove
that a handler is mounted or a branch is admitted.

`handler_dispatch` records method/path if conditions, method switches and case
conditions with their operators preserved. This catches changes such as GET
becoming POST or a compound path condition changing while the same path literal
remains. Negative guards remain rejection evidence; the tool does not turn them
into claimed admitted operations.

The platform caller audit uses `go run ./scripts/api-inventory
-platform-consumers` from OpenCore and writes the combined evidence to
`tasks/pivot-closeout/proof/a01-platform-inventory.json`. Add `-check` to reject
drift without writing. It includes sibling `../verification` and `../tools`
trees, with paths relative to OpenCore and the same count/hash rules. This
private proof stays outside the public runtime's generated contract; the normal
inventory and check remain reproducible in an isolated public checkout.

Registration entries retain their handler expression, including inline handler
bodies. This catches changing a mounted handler on a different source line even
when the path, method and namespace-reference hash stay unchanged. Expressions
that are local variables remain source wiring evidence; they do not establish
transitive handler reachability or resolve application method dispatch.

`handler_bindings` retains local definitions and identifier alias chains behind
registered handler variables, including constructor `(handler, error)` results.
Assignments in branches are all retained; this is evidence of possible wiring,
not an assertion that every branch executes. Alias cycles terminate. Constructor
arguments remain dependencies rather than being expanded as handler aliases.

Dispatch conditions include their lexical `context`: enclosing then/else
conditions, switch subjects and case/default membership. Moving a method branch
to another path branch therefore changes the baseline. Context preserves source
structure; it does not infer control-flow effects of earlier returning guards.

`mounted-route-review.json` is the reviewed reconciliation of every registration
with its dispatch source and method contract. The inventory suite requires each
mount exactly once, with its current deletion owner and an existing source file.
Nested Workspace/access muxes, OAuth callback, browser middleware, protocol
parsers and SPA fallback retain distinct evidence; static branch conditions do
not establish successful execution or resource availability. Method changes are
also caught by the contextual generated dispatch baseline.
