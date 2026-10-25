# MCP runtime

The only durable upstream identity is a typed Node **Connection**. A Workspace
mount references that Connection and stores its local alias and explicit tool
allowlist. No MCP source registry, source-scoped credential store or separate
source-management API is mounted or read at daemon startup.

`OpenHTTPConnectionBinding` and `OpenStdioConnectionBinding` own the upstream
sessions. Credential resolution is scoped to the referenced Connection; no
secret is persisted in a mount or exposed in catalog/evidence. HTTP discovery
uses the guarded SDK client, while stdio runs a configured executable without
a shell and owns child shutdown. Failures of one upstream do not fabricate
readiness for that mount or prevent unrelated mounts from operating.

Catalogs are derived from current upstream tool descriptors intersected with
the Workspace's explicit allowlist. Newly discovered tools cannot become
callable without an operator change. A reviewed catalog is revalidated before
dispatch, including Connection identity and revision. MCP invocation evidence
retains exact Connection revision, mount identity and provider outcome.

Primary proof is through the Connection-backed daemon journeys, MCP catalog
and execution tests; legacy source registry tests have been removed with the
registry they exercised.

## Package boundary for built-in sources

`internal/mcp` owns protocol transport, immutable Workspace catalogs, source
binding and dispatch. Node administration tool adapters live in
`internal/adapters/inbound/adminmcp`; `internal/bootstrap` composes them into
the daemon-owned source. A future built-in MCP capability can occupy its own
narrowly named inbound adapter package *when implemented*. Do not expand the
Admin tool package into a catch-all, create a new MCP server/permission plane,
or introduce a dynamic source registry in anticipation of hypothetical tools.
This Windows-filesystem slice does not add any other MCP source.
