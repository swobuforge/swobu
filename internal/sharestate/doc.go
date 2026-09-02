// Package sharestate owns daemon-private Shared Routes Endpoint identity,
// replaceable application-TLS credentials, certificate lifecycle state, and
// bearer Grants. A missing state file is a valid cold Store; EnsureEndpoint
// creates only the stable identity key. EndpointID and hostname derive solely
// from that key. TLSCredential is absent or atomically complete and may be
// replaced without changing Endpoint identity. The package does not own route
// semantics, Relay sessions, or ACME execution.
package sharestate
