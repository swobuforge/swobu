// Package operatorclient owns daemon-backed operator client access for endpoint,
// auth, and status workflows.
//
// All operator surfaces should use this package for daemon HTTP control-plane
// access rather than issuing raw requests.
package operatorclient
