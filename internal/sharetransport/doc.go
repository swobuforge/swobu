// Package sharetransport implements the Owner side of the Shared Routes reverse
// transport. It owns Owner transport TLS, yamux sessions, certificate
// provisioning messages, application TLS termination, and cold runtime
// reconciliation. Hosted Relay routing, issuance policy, ACME accounts,
// deployment, and operations live outside OpenCore. Certificate renewal reuses
// the same Endpoint key and derives timing from the installed certificate
// rather than persisted scheduler state.
package sharetransport
