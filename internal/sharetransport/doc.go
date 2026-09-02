// Package sharetransport implements the Owner side of the Shared Routes reverse
// transport. A stable identity key authenticates Owner transport; every Endpoint
// certificate acquisition generates a fresh application-TLS key and commits it
// only with its validated certificate chain. The package also owns yamux
// sessions, certificate control messages, application TLS termination, and cold
// runtime reconciliation. Hosted Relay routing, issuance policy, ACME accounts,
// deployment, and operations live outside OpenCore.
package sharetransport
