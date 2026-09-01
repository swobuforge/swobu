// Package sharestate owns daemon-private Shared Routes Endpoint identity,
// public certificate material, and bearer Grants. A missing state file is a
// valid cold Store; EnsureEndpoint owns the first persisted key transition.
// EndpointID, hostname, and renewal time are derived from the Endpoint key and
// certificate. The package does not own route semantics, HTTP protocol codecs,
// Relay sessions, or ACME execution.
package sharestate
