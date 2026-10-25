// Package configstore owns the current schema-5 routing document and its
// immutable Workspace snapshots. Managed writes serialize through one path
// lock, validate the complete routing graph, replace YAML atomically, then
// publish the new snapshot in memory. Model routes are an ordered sequence:
// no map/order reconciliation, implicit sorting or historical schema decoding.
// Connection admission is installed at startup. The registry read lock remains
// held through the Node file write, so reviewed Connection revisions cannot
// change after admission and before commit. Workspace MCP mounts and Search
// persist only Connection references. Search stores one primary and an optional
// ordered fallback list; historical single-Connection schema-5 YAML remains valid.
// MCP mount applies bind the exact Workspace and reviewed Connection revisions.
// They have no replay ledger; uncertain delivery is recovered by reading the
// current Workspace before another explicit mutation.
//
// The local host (including processes able to reach loopback Control) is trusted.
// New state is created with restrictive OS mode requests where supported;
// existing user-selected paths are not rejected or modified for ACL shape.
// The config lock and atomic replacement defend ordinary concurrent writes,
// not against malicious local processes.
//
// OpenReadOnly consumes an existing document without permission changes,
// locks or writes; mutation APIs fail before admission or precommit callbacks.
// History migration is not part of startup. Only the current schema is read.
// Service limits are optional schema-5 Workspace values. Checked edits bind the
// same Workspace incarnation/revision and recheck admitted authority at publish;
// a no-op preserves both document bytes and the management revision.
package configstore
