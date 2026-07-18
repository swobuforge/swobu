// Package configstore owns Swobu's only durable routing configuration format
// and transactional local file store.
//
// YAML DTOs remain private here. Store updates serialize semantic edits against
// the latest immutable routing.Config, validate the whole next aggregate,
// atomically replace the file, and publish at the rename commit point. Directory
// sync is attempted afterward as best-effort durability; failure is warned and
// does not turn a committed update into an error. A portable lifetime path lock
// and explicit closed state enforce the single-daemon writer contract.
// OpenOrCreate owns initial directory and canonical empty-document creation
// under that same lock; startup path resolvers never write routing files.
package configstore
