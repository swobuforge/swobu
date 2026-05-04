# swobucli/oss

Public runtime and release surface for Swobu.

## Scope

- runtime source (`cmd/`, `internal/`)
- release/build wiring (`Makefile`, `.goreleaser.yml`, release scripts)
- public installers (`scripts/install.sh`, `scripts/install.ps1`)

No standalone OSS test plane; tests are collocated Go tests (`*_test.go`).

## Public Entrypoints

- `make -C swobucli/oss` — list public OSS entrypoints
- `make -C swobucli/oss test` — deterministic required tests
- `make -C swobucli/oss build` — local binary build (`.out/swobu`)
- `make -C swobucli/oss artifacts SWOBU_VERSION=vX.Y.Z` — build release archives + checksums

## Installer Contract

Installers must always verify release archive integrity via `checksums.txt`.

## License

This project is licensed under GNU Lesser General Public License v3.0 only (`LGPL-3.0-only`).
See `COPYING.LESSER` and `COPYING`.

Commercial licensing is available via licensing@swobu.com.

Copyright (c) 2026 Dmytrii Shchadei.
