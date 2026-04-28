// Package ptykit provides a reusable PTY harness for test and dev automation.
//
// Intent:
//   - Keep PTY orchestration consistent across TUI and integration tests.
//   - Maintain a terminal-emulated visible viewport alongside raw PTY bytes so
//     tests can assert what operators actually see.
//   - Reduce duplicated key-send and output-wait plumbing.
//   - Keep PTY support in the test lane rather than production packages.
package ptykit
