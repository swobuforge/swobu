# Workspace MCP endpoint

A Workspace with MCP enabled exposes Streamable HTTP at
`/workspaces/{slug}/mcp`. Its explicit mounts compose external MCP Connections
and the daemon-owned `swobu-admin` source. Models routes are optional.

## Mounting sources

Use Web Operator → Workspace → Services → MCP → Add server. Choose an MCP
Connection or **Swobu Node administration · builtin**, enter a namespace and
select exact tool names. Review source identity, permissions, affected
Principals and publication before applying. External sources need declared
execution identity and permission summary. Discovery never extends an allowlist.

The equivalent desired configuration is:

```yaml
schema_version: 5
workspaces:
  operator-tools:
    enabled_services: [mcp]
    routes: []
    mcp_mounts:
      node:
        builtin: swobu-admin
        allowed_tools: [workspaces_list, workspace_get]
```

Each mount selects exactly one `builtin` or `connection_id`. The builtin has no
Connection, endpoint, process, credential locator or special Workspace type.
External Connection names and aliases cannot select builtin authority. Workspace
catalog discovery opens only its mounted sources; an unrelated unreachable
Connection cannot prevent builtin recovery operations.

## Admission and administration

Configure the MCP client with this URL and an ordinary Principal credential
issued for this Workspace. Its current grant must include MCP; an existing broad
Workspace grant also includes MCP. Mounting Admin tools therefore delegates Node
administration to those Principals. Prefer a dedicated ordinary MCP-only
Workspace and explicit MCP grants. Publication controls relay reachability;
credentials, grants, Services, mounts and limits still apply independently.

The tool namespace is `<alias>__<tool>`, for example `node__workspaces_list`.
Tools invoke canonical Node commands as the admitted Principal, with live
credential, grant, mount and public-publication checks at mutation time. Original
ETags, review digests and caller-scoped retry receipts remain authoritative.
A managed bearer cannot enter `/api/v1` Control directly. Self-revocation,
self-unmount and self-unpublication complete once; later affected calls fail.
The local authenticated Web Operator/CLI remains available for recovery.

See [Admin workflows](admin-mcp.md) for tools, reviewed writes and secure host
handoffs. There is no standalone `swobu mcp` server, Admin session, Node bearer
or `/admin/mcp` endpoint. Ordinary Models requests do not acquire Admin tools.

## Transport and evidence

The pinned official SDK negotiates protocol revision during initialization;
clients must follow that version. The HTTP surface is stateless, requires no MCP
session ID, and returns `Cache-Control: no-store`. Untrusted Origins, ambiguous
paths and unauthorized callers fail before dispatch. Upstream credentials stay
with their owning Connection and declared origin; guarded HTTP discovery rejects
unsafe destinations and redirects. Refresh never retains a stale credential
binding after a Connection revision change.

Activity records standalone MCP lifecycle and true Principal attribution,
without arguments, results, prompts or invented model costs. Missing evidence
or exhausted retained pages does not prove no execution or complete capture.

Executable proofs cover the assembled daemon, separate compiled daemon with an
external SDK client, Owner stream/application TLS, revocation, restart and local
recovery. Production relay enrollment/DNS and platform release qualification
require their own receipts; fixture transport proof does not establish them.
