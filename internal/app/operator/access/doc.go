// Package access owns operator use cases for managed Access identities. It
// validates Workspace existence under the shared routing-binding coordinator
// before issuing an immutable credential grant.
// Workspace-use permission deadlines publish through the same Access authority
// transaction and project independently of credentials. No expiry is inferred
// from a credential, and receipt replay cannot renew a relationship.
// Permission preview validates Workspace existence under the shared coordinator
// and delegates the exact proposed edit to Access state, without publishing it.
// Permission reads distinguish absent grants from missing resources and never
// expose credential material or turn inspection into permission issuance.
// Checked Control mutations retain trusted admission through coordinator waits
// and repeat it under the Access transaction lock before apply or replay.
// GET, preview and checked apply share the natural relationship revision;
// unrelated Principal mutations do not stale consent and whole-store ETags
// cannot authorize a fresh checked mutation.
// Mutation results project the locked store snapshot without post-apply reads;
// replay acknowledgement does not claim a historical grant remains active.
// Credential issuance Control commands require a stable retry key and repeat
// local admission in the Access publication lock. Replay projects original
// metadata with secret_not_recoverable, not successful secret delivery. Trusted
// Principal detail preserves the listing projection and revoked identity for
// attribution. Credential collections expose retained ID/audience/time metadata
// only; no digest or plaintext crosses the operator read boundary.
// Canonical Principal creation publishes only identity metadata through checked
// local admission. Permission grant and credential issuance remain independent;
// matching retry projects current identity rather than resurrecting authority.
// Principal projections carry the resource-specific generation revision. Label
// updates return the locked committed projection and never modify authority.
// Principal revocation preview projects current permission scope and credential
// count without secrets. Apply publishes identity-wide revocation and its retry
// receipt atomically, returning the locked current projection and replay truth.
// Credential revocation preview delegates exact selected impact/revision to
// Access. Checked apply projects the locked removal/receipt result, preserving
// permission and sibling scope. Replay acknowledges current state, not a fresh
// removal, credential replacement or recovered secret.
package access
