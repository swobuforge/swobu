// Package adapters translates daemon operator control-plane data into Cockpit
// ports and read models.
//
// It is the only Cockpit package allowed to know the concrete operator client.
// UI packages consume ports and readmodel snapshots; they do not import daemon
// clients or endpoint-intent transport details. Daemon-returned connection
// drafts are presentation facts: adapters preserve them structurally and never
// reconstruct validated routing connections in the Cockpit process. Save and
// probe drafts cross the operator boundary raw so the daemon environment owns
// filesystem-bearing credential validation. Zero authoritative workspaces
// are projected here as the Cockpit-only conventional `default` workspace;
// projection performs no workspace get/create call. Activity projection keeps
// requested model, routing-owned route identity, and execution-time terminal
// provider/model separate. Active Share summaries are joined only onto routes
// already present in daemon workspace truth; stale Grants cannot create routes.
package adapters
