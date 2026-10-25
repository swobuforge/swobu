// Package httpapi exposes separate local Operator and Workspace data planes.
//
// Local Operator Control requires the live built-in host authority and browser
// session protection. Workspace credentials cannot escalate to Control, even
// through unknown or retired paths. Mutations repeat trusted admission at the
// owning store's atomic publication boundary.
// Retained Activity filters and CSV/NDJSON downloads reuse Ledger's indexed
// queries and content-free projection. Export belongs only to local Control:
// two concurrent reads, a 30-second query deadline, 2000 rows and an 8 MiB
// successful body per response. It buffers before success, preserves cursor
// scope and never certifies complete capture or an atomic snapshot.
//
// Public relay ingress mounts ONLY canonical published Workspace data paths.
// It never exposes browser assets or Node Control. The publication store
// answers reachability; Access owns bearer authentication, Service grants,
// revocation and per-Principal request limits. Every public data operation
// requires a current Workspace credential; no anonymous bypass exists.
//
// Model and MCP streams recheck authority at effect time; MCP tool calls and
// direct Search are separately admitted and metered. Public CORS preflight
// does not grant authentication or expand the protocol operation allowlist.
// Malformed paths, forwarded host headers and ambiguous credentials confer
// no authority.
//
// Publication HTTP is a small local-control projection over the single
// Node-owned sorted Workspace membership set: GET remote addresses, GET a
// Workspace's publication, PUT or DELETE that Workspace's built-in relay
// publication at an exact strong revision. Publish holds a relay-readiness
// lease until the Node document commit; no parallel Share/Exposure journal
// or independent published-protocol mask exists.
// Workspace service-limits Control GET/PUT uses the strong Workspace ETag and
// checked command. One daemon-owned limiter spans local/public Models and direct
// Search GET/POST; Responses WebSocket charges each create, while MCP charges tools/call
// at invocation. Permission/origin checks precede operational admission.
// Responses WebSocket scheduling is connection-local: named lanes overlap,
// each lane is FIFO, and every dispatch rechecks admission. Disconnect cancels
// and joins all workers; transport lane IDs never become Exchange state.
package httpapi
