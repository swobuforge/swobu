// Package runtime implements the framework application loop: input translation,
// app update, effect execution, compilation, and frame delivery.
//
// It depends on internal/tui/core and internal/tui/compiler only.
// No real terminal I/O in this slice; a fake terminal adapter is used for tests.
package runtime
