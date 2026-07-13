// Package fixture owns deterministic fixture-backed visual comparison for
// terminal screen snapshots.
//
// Boundary law:
//   - reads/writes test-local fixture files only
//   - no test runtime concerns (retry, timing, pty, daemon)
//   - promotion gate via env var
package fixture
