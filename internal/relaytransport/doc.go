// Package relaytransport carries public Workspace requests over Owner relay streams.
// The relay forwards encrypted bytes; application TLS terminates at the Node's
// data-only handler, which owns current publication and Access admission.
//
// OwnerRuntime holds a readiness lease through each publication commit. Active
// publication membership or outstanding leases keep transport running; removing
// the last membership stops it after leases drain. Identity and TLS replacement
// persist through relaystate, independently of Workspace credentials and grants.
// This package neither mounts Node Control nor issues public authorization.
package relaytransport
