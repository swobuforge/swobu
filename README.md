# swobucli/oss

Public runtime and release surface for Swobu.

## Scope

- runtime source (`cmd/`, `internal/`)
- release/build wiring (`Makefile`, `.goreleaser.yml`, release scripts)
- public installers (`scripts/install.sh`, `scripts/install.ps1`)

No standalone OSS test plane; tests are collocated Go tests (`*_test.go`).

## Public Entrypoints

- `make -C swobucli/oss` — list public OSS entrypoints
- `make -C swobucli/oss verify` — merge-safety gate
- `make -C swobucli/oss test` — deterministic required tests
- `make -C swobucli/oss build` — local binary build (`.out/swobu`)
- `make -C swobucli/oss publish patch|minor|major` — tag and push release
- `make -C swobucli/oss verify-published` — verify published release/install surfaces
- `make -C swobucli/oss run` — run operator surface

## Install Command Contract

Install command text advertised by product surfaces must come from one canonical installer URL and must pass integration verification (`verify-published` and site integration command test).
