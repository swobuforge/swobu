// Command codelint runs Swobu's repo-owned production-code size cap checker.
//
// Generic Go static analysis belongs to the maintained golangci-lint profile.
// Repo-specific hard caps for current product code belong to
// internal/devtools/codelint and are exposed through this thin command
// entrypoint.
package main
