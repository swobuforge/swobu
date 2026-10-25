// Package relaystate owns private relay transport identity and certificate state.
// Its relay.json journal contains endpoint keys, TLS credentials and certificate
// retry facts only. Workspace publication belongs to the Node YAML document;
// Access alone owns user credentials, permissions and revocation.
//
// Opening a journal accepts only the current schema without rewriting host-owned
// file permissions. Newly created state requests owner-only permissions, but
// local file modes are not an authorization boundary. Neither path translates
// Share state or grants reachability.
// Callers must never log snapshots containing private keys or certificate material.
package relaystate
