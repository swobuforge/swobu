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
