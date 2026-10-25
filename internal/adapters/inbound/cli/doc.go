// Package cli owns local shell operations and projects validated Node and
// Workspace management through the same checked domain commands used by the
// Web Operator. Share and Exposure commands do not exist.
//
// Destructive commands require original revision-bound intent and explicit
// user confirmation; partial acknowledgements are never inferred as success.
// Local Node selection prefers a leaf --addr, then Runner.Addr, then the
// environment/default, with loopback validation before contacting Control.
// Daemon config selection follows the same precedence through Runner.ConfigPath.
// The binary enumerates commands; this package owns their argument parsing.
package cli
