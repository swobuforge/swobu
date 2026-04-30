# swobucli

This directory is the product root for the Swobu CLI/daemon surface.

## Scope

- operator-facing CLI and daemon product surface
- compatibility/runtime behavior owned by existing Go runtime roots

## Runtime mapping

The runtime implementation is rooted inside `swobucli`:

- `swobucli/cmd/swobu`
- `swobucli/internal/*`
- `swobucli/test/*`

## Entrypoints

- `make -C swobucli verify`
- `make -C swobucli test`
- `make -C swobucli run`

## Install

- latest:
  - `curl -fsSL https://raw.githubusercontent.com/metrofun/swobu/main/swobucli/scripts/install.sh | sh`
- pinned:
  - `curl -fsSL https://raw.githubusercontent.com/metrofun/swobu/main/swobucli/scripts/install.sh | sh -s -- --version vX.Y.Z`
- custom bin dir:
  - `curl -fsSL https://raw.githubusercontent.com/metrofun/swobu/main/swobucli/scripts/install.sh | sh -s -- --bin-dir "$HOME/.local/bin"`
- windows (PowerShell, latest):
  - `irm https://raw.githubusercontent.com/metrofun/swobu/main/swobucli/scripts/install.ps1 | iex`
- windows (PowerShell, pinned):
  - `& ([scriptblock]::Create((irm https://raw.githubusercontent.com/metrofun/swobu/main/swobucli/scripts/install.ps1))) -Version vX.Y.Z`

The installers only download release archives, verify SHA256 against `checksums.txt`, and install the `swobu` binary.
