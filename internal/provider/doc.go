// Package provider defines the boundary for one outbound provider attempt.
//
// Adapters encode, send, decode, and classify failures. Exchange orchestration
// owns routing and recovery. A failure's availability classification alone
// does not establish that retrying an issued request is safe.
package provider
