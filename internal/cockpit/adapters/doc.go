// Package adapters translates daemon operator control-plane data into Cockpit
// ports and read models.
//
// It is the only Cockpit package allowed to know the concrete operator client.
// UI packages consume ports and readmodel snapshots; they do not import daemon
// clients or endpoint-intent transport details.
package adapters
