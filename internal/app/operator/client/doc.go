// Package operatorclient implements the local daemon's typed control client.
// Operators may inspect and change Workspaces, Connections, Access, Limits and
// Activity through their respective canonical endpoints. It does not own
// networking, credentials, authorization or publication state.
//
// Checked commands preserve original ETags and, where their owner has a retry
// contract, retry identities through uncertain transport outcomes. MCP mount
// writes have no replay contract: read the Workspace after uncertain delivery.
// Immutable intent never acts as renewed authorization.
// Retrieved management audits are redacted historical facts, not live
// permissions or proof that a client configuration currently works.
package operatorclient
