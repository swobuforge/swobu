// Package adapters translates daemon operator control-plane data into Cockpit
// ports and read models.
//
// It is the only Cockpit package allowed to know the concrete operator client.
// UI packages consume ports and readmodel snapshots; they do not import daemon
// clients or endpoint-intent transport details. Zero authoritative workspaces
// are projected here as the Cockpit-only conventional `default` workspace;
// projection performs no workspace get/create call. Activity projection keeps
// requested model, routing-owned route identity, and execution-time terminal
// provider/model separate.
package adapters
