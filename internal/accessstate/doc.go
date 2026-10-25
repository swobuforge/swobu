// Package accessstate owns Principals, their Workspace permissions, and
// credentials. There is one authority: the current Principals section of the
// Node's atomic swobu.yaml document, published by the Node file owner.
// Only schema 3, explicit permission arrays and positive Principal authority
// generations are admitted. No standalone access.json, historical schema or
// startup migration exists.
//
// A credential belongs to exactly one Workspace. It does not create a grant:
// every request rechecks the Principal's current permission, Service scope,
// expiry and applicable request-rate ceiling. Credential revocation and
// permission revocation have independent lifecycles. Workspace destruction
// revokes both for that Workspace, even if its slug is subsequently reused.
//
// Management mutations compare an owner-stamped Principal or relationship
// revision while holding the publication lock. Failed publication changes
// neither in-memory authority nor retained retry information. Checked retry
// receipts (which never contain secrets) are stored in that same document;
// replays never reissue credentials or reauthorize a revoked relationship.
// Advisory previews perform no publication and cannot confer permission.
//
// Existing credentials and Principal metadata remain inspectable after grant
// revocation for attribution, never as evidence of current authority.
package accessstate
