// Package operatorclient owns daemon-backed operator client access for endpoint,
// auth, and status workflows.
//
// This package is the runtime lane only. Static launch profiles, run-command
// rendering, and client capability declarations live in sibling package
// `clientprofile`.
//
// All operator surfaces should use this package for daemon HTTP control-plane
// access rather than issuing raw requests.
package operatorclient
