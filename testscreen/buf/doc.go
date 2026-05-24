// Package buf owns the read-only terminal screen buffer contract for tests.
//
// Boundary law:
//   - immutable observation surface only
//   - no timing/retry policy
//   - no fixture IO
//   - no runtime-driver concerns (pty, vt, retained loop)
package buf
