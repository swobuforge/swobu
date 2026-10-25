# Workspace administration by MCP

Mount `swobu-admin` in an ordinary MCP-enabled Workspace, review its exact tool
allowlist and grant the intended Principal MCP access. Connect with its ordinary
Workspace credential at `/workspaces/{slug}/mcp`.
[Workspace MCP](workspace-mcp.md) describes setup and admission. With namespace
`node`, tool names are published as `node__<name>`.

| Outcome | Typed tools |
|---|---|
| Node and Workspaces | `node_status`, `workspaces_list`, `workspace_get`, `workspace_create`, `workspace_rename`, `workspace_delete` |
| Services | `workspace_services`, `workspace_service_set` |
| Models | `model_route_get`, `model_route_put`, `model_route_delete`, `model_route_order`, `model_match` |
| Connections | `connections_list`, `connection_get`, `connection_references`, `connection_create`, `connection_check`, `connection_delete` |
| MCP | `mcp_catalog`, `mcp_mounts`, `mcp_mount`, `mcp_mount_delete`, `workspace_tools_list` |
| Search | `search_route_get`, `search_route_set` (empty Connection clears it) |
| Access | `principals_list`, `principal_get`, `principal_create`, `principal_rename`, `workspace_permission_get`, `permission_set`, `principal_credentials_list`, `credential_revoke` |
| Limits | `workspace_service_limits_set` (Workspace-wide request/concurrency admission), `workspace_limits_status`, `workspace_limit_set`, `workspace_limit_clear` (estimated spend) |
| Publication | `workspace_publication_get`, `workspace_publication_set`, `remote_addresses_list` |
| Connect | `clients_list`, `client_preview`, `client_apply_handoff` |
| Activity | `activity_get`, `activity_operation_get` |
| Secret custody | `credential_issue_handoff`, `connection_create_handoff`, `connection_credentials_handoff` |

Inspect the published schema for required arguments. Unknown arguments and
generic shell/HTTP dispatch are rejected. Use `workspace_get` for its management
ETag; individual Principal, Connection, accounting and publication reads return
their own revisions. They are not interchangeable.

Creation carries a stable retry key and returns original canonical receipt
replay. Reviewed operations use `dry_run=true`, then the same intent with
`dry_run=false` and the original review/revision. Advisory preview is not a
second approval authority or evidence of effect. After uncertainty, inspect
current state and recover the original intent; never manufacture fresh consent
or blindly resend a mutation. Existing Node owners decide conflicts and effects.

## Secure handoffs

A handoff returns `requires_local_operator`, an authenticated local Operator path
and a verification tool. It does not claim completion or create a session. The
human completes the requested action on the Node host; provider secrets and
one-shot bearers stay outside tool arguments, results, Activity and chat.

Credential issuance uses Access detail or `swobu credential issue --help`.
After local issuance, match the intended Principal/Workspace and newly created
credential ID using `principal_credentials_list`. Metadata cannot recover a
secret or prove a current grant. Authenticated provider creation/rotation uses
Connections and the supported host credential seam; follow with public readback
and an explicit `connection_check`. Direct `connection_create` accepts only
credential-free HTTP MCP, Search and custom Models configuration.

Connect preview validates the chosen existing Principal, Workspace Models grant,
concrete model ID and supported local client. MCP receives decisions and changed
field names; private file paths and before/after values are omitted. The human
reviews and applies in Workspace → Connect. Call `client_preview` again for the
same tuple and require `already_configured=true`; this includes acceptance of
the exact installed credential, without invoking a model.

For terminal host execution, use the installed command's help and retained intent:

```sh
swobu connect codex --apply --workspace coding --model coding --principal '<principal-id>' --intent-file '<private-intent-file>'
swobu connect codex --recover --workspace coding --model coding --principal '<principal-id>' --intent-file '<same-private-intent-file>'
```

The Node must already be running. Keep the original private intent file outside
agent context. Local dry-run JSON includes private before-values and paths and
must not be forwarded to a model as an MCP result. Apply/recovery preserve the
original reviewed destination and retry identity, and require local admission.

## Evidence boundaries

Limits retain decimal-string totals, coverage gaps and uncertain holds. They are
soft estimates, not provider invoices or guaranteed maximum charges. Activity
keeps standalone MCP operations separate from model Exchanges, with current and
retained evidence distinguished. Optional retained-page cursors bind Workspace
and operation kind. Empty results never establish complete capture. Node Activity
streaming remains a local finite observation transport, not a long-running tool.
Management audit retains original admitted actors and canonical owner receipts.

Settings History and Administration do not yet expose mutable controls. Retained
Activity downloads are available from the local Control API, using the same
content-free Ledger projections as `activity_get`; see [Activity exports](../activity.md).
Agents can page `activity_get` without granting arbitrary file download or raw
Ledger access through MCP. Node status and remote-address reads cover the live
read outcomes of the remaining placeholder pages.
